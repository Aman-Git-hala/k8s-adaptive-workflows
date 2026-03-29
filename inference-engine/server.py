#!/usr/bin/env python3
"""
server.py — gRPC Inference Engine server.

Implements the InferenceService from proto/inference.proto:
  - Predict():       Loads the ONNX model, runs inference, returns per-task predictions
  - ReportMetrics(): Stores actual execution data in PostgreSQL for future learning

Features:
  - Cold-start fallback: if no history, uses BaseResources hints or defaults
  - ONNX Runtime for fast, portable inference
  - PostgreSQL integration for the feedback loop

Environment Variables:
  ONNX_MODEL_PATH   — Path to the ONNX model file (default: model.onnx)
  DB_CONNECTION_STR  — PostgreSQL connection string (optional; no-op if unset)
  GRPC_PORT          — Port to listen on (default: 50051)
"""

import hashlib
import logging
import os
import sys
import time
from concurrent import futures

import grpc
import numpy as np

# Add proto stubs to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "proto", "gen", "python"))

import inference_pb2
import inference_pb2_grpc

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
logger = logging.getLogger("inference-engine")

# ───────────────────────────────────────────────────────────────
# Default resource predictions (cold-start fallback)
# ───────────────────────────────────────────────────────────────
DEFAULT_CPU_MILLICORES = 100
DEFAULT_MEMORY_MIB = 128
DEFAULT_DURATION_S = 30.0
DEFAULT_CONFIDENCE = 0.1


def image_hash(name: str) -> float:
    """Same hash function as train_model.py — must match."""
    h = hashlib.sha256(name.encode()).hexdigest()
    return int(h[:8], 16) / (16**8)


class InferenceServicer(inference_pb2_grpc.InferenceServiceServicer):
    """gRPC InferenceService implementation."""

    def __init__(self, model_path: str, db_conn_str: str | None = None):
        self.model_path = model_path
        self.onnx_session = None
        self.db = None

        # Load ONNX model if available.
        self._load_model()

        # Connect to PostgreSQL if configured.
        if db_conn_str:
            self._connect_db(db_conn_str)

    def _load_model(self):
        """Load the ONNX model for inference."""
        if not os.path.exists(self.model_path):
            logger.warning(f"ONNX model not found at {self.model_path}. Using cold-start defaults.")
            return

        try:
            import onnxruntime as ort
            self.onnx_session = ort.InferenceSession(self.model_path)
            logger.info(f"Loaded ONNX model from {self.model_path}")
        except Exception as e:
            logger.error(f"Failed to load ONNX model: {e}. Using cold-start defaults.")

    def _connect_db(self, conn_str: str):
        """Connect to PostgreSQL for historical data."""
        try:
            import psycopg2
            self.db = psycopg2.connect(conn_str)
            self.db.autocommit = True
            logger.info("Connected to PostgreSQL for historical metrics")
        except Exception as e:
            logger.warning(f"Failed to connect to PostgreSQL: {e}. Running without history.")

    def _get_history(self, image: str) -> tuple[float, float, float, int]:
        """
        Query historical averages for an image.
        Returns (avg_cpu, avg_mem, avg_duration, sample_count).
        """
        if not self.db:
            return 0.0, 0.0, 0.0, 0

        try:
            cur = self.db.cursor()
            cur.execute(
                """
                SELECT COALESCE(AVG(actual_cpu_m), 0),
                       COALESCE(AVG(actual_mem_mib), 0),
                       COALESCE(AVG(duration_s), 0),
                       COUNT(*)
                FROM task_executions
                WHERE image = %s AND succeeded = TRUE
                  AND completed_at > NOW() - INTERVAL '30 days'
                """,
                (image,),
            )
            row = cur.fetchone()
            cur.close()
            return row if row else (0.0, 0.0, 0.0, 0)
        except Exception as e:
            logger.error(f"Failed to query history: {e}")
            return 0.0, 0.0, 0.0, 0

    def Predict(self, request, context):
        """Handle a Predict RPC — return per-task resource predictions."""
        logger.info(f"Predict called for workflow '{request.workflow_name}' with {len(request.tasks)} tasks")

        predictions = {}

        for task in request.tasks:
            # Get historical data for this image.
            hist_cpu, hist_mem, hist_dur, sample_count = self._get_history(task.image)

            # Determine prediction method.
            if self.onnx_session and sample_count > 0:
                # ML prediction: use ONNX model with historical features.
                pred = self._ml_predict(task, hist_cpu, hist_mem, hist_dur)
                confidence = min(0.9, 0.5 + sample_count * 0.05)
            elif sample_count > 0:
                # No model, but have history: use raw averages.
                pred = inference_pb2.TaskPrediction(
                    cpu_millicores=int(hist_cpu * 1.2),  # 20% headroom
                    memory_mib=int(hist_mem * 1.2),
                    estimated_duration_s=hist_dur * 1.1,
                    confidence=min(0.7, 0.3 + sample_count * 0.04),
                )
                confidence = pred.confidence
            elif task.base_resources and (task.base_resources.cpu_millicores > 0 or task.base_resources.memory_mib > 0):
                # Cold start with user hints.
                pred = inference_pb2.TaskPrediction(
                    cpu_millicores=task.base_resources.cpu_millicores if task.base_resources.cpu_millicores > 0 else DEFAULT_CPU_MILLICORES,
                    memory_mib=task.base_resources.memory_mib if task.base_resources.memory_mib > 0 else DEFAULT_MEMORY_MIB,
                    estimated_duration_s=DEFAULT_DURATION_S,
                    confidence=0.3,
                )
                confidence = 0.3
            else:
                # Full cold start: use defaults.
                pred = inference_pb2.TaskPrediction(
                    cpu_millicores=DEFAULT_CPU_MILLICORES,
                    memory_mib=DEFAULT_MEMORY_MIB,
                    estimated_duration_s=DEFAULT_DURATION_S,
                    confidence=DEFAULT_CONFIDENCE,
                )
                confidence = DEFAULT_CONFIDENCE

            predictions[task.name] = pred
            logger.info(
                f"  Task '{task.name}' (image={task.image}): "
                f"cpu={pred.cpu_millicores}m, mem={pred.memory_mib}Mi, "
                f"dur={pred.estimated_duration_s:.1f}s, conf={confidence:.2f}"
            )

        return inference_pb2.PredictResponse(predictions=predictions)

    def _ml_predict(self, task, hist_cpu: float, hist_mem: float, hist_dur: float):
        """Run ONNX inference."""
        features = np.array(
            [[image_hash(task.image), hist_cpu, hist_mem, hist_dur]],
            dtype=np.float32,
        )

        input_name = self.onnx_session.get_inputs()[0].name
        outputs = self.onnx_session.run(None, {input_name: features})
        pred_cpu, pred_mem, pred_dur = outputs[0][0]

        return inference_pb2.TaskPrediction(
            cpu_millicores=max(10, int(pred_cpu * 1.1)),  # 10% headroom
            memory_mib=max(8, int(pred_mem * 1.1)),
            estimated_duration_s=max(1.0, float(pred_dur)),
            confidence=0.0,  # Will be set by caller
        )

    def ReportMetrics(self, request, context):
        """Handle a ReportMetrics RPC — store actual execution data."""
        logger.info(
            f"ReportMetrics called for workflow '{request.workflow_name}' "
            f"with {len(request.task_metrics)} metrics"
        )

        if not self.db:
            logger.warning("No database connected. Metrics will be discarded.")
            return inference_pb2.ReportMetricsResponse(accepted=False)

        try:
            cur = self.db.cursor()
            for m in request.task_metrics:
                cur.execute(
                    """
                    INSERT INTO task_executions
                        (workflow_name, task_name, image, actual_cpu_m, actual_mem_mib, duration_s, succeeded, completed_at)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
                    """,
                    (
                        request.workflow_name, m.task_name, m.image,
                        m.actual_cpu_millicores, m.actual_memory_mib,
                        m.actual_duration_s, m.succeeded,
                        m.completed_at or time.strftime("%Y-%m-%dT%H:%M:%SZ"),
                    ),
                )
            cur.close()
            logger.info(f"Stored {len(request.task_metrics)} metrics")
            return inference_pb2.ReportMetricsResponse(accepted=True)
        except Exception as e:
            logger.error(f"Failed to store metrics: {e}")
            return inference_pb2.ReportMetricsResponse(accepted=False)


def serve():
    """Start the gRPC server."""
    model_path = os.getenv("ONNX_MODEL_PATH", "model.onnx")
    db_conn_str = os.getenv("DB_CONNECTION_STR")
    port = os.getenv("GRPC_PORT", "50051")

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    servicer = InferenceServicer(model_path=model_path, db_conn_str=db_conn_str)
    inference_pb2_grpc.add_InferenceServiceServicer_to_server(servicer, server)

    listen_addr = f"[::]:{port}"
    server.add_insecure_port(listen_addr)
    server.start()

    logger.info(f"Inference Engine gRPC server listening on {listen_addr}")
    logger.info(f"  ONNX model: {model_path} ({'loaded' if servicer.onnx_session else 'NOT FOUND'})")
    logger.info(f"  PostgreSQL: {'connected' if servicer.db else 'not configured'}")

    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        logger.info("Shutting down...")
        server.stop(grace=5)


if __name__ == "__main__":
    serve()
