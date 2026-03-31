# Inference Engine (Python)

A standalone Python gRPC microservice that provides **ML-based resource predictions** for the K8s Adaptive Workflows project. It uses an ONNX model (trained with scikit-learn) to predict CPU, memory, and duration requirements for workflow tasks based on historical execution data.

## Architecture

```
Controller (Go) ──gRPC:50051──▶ Inference Engine (Python)
                                  │
                                  ├── Predict():       ONNX model inference
                                  ├── ReportMetrics(): Store actual metrics
                                  │
                                  └── PostgreSQL ◀──── Historical data
```

## Prediction Pipeline

Three prediction paths (in priority order):

| Priority | Method | Confidence | When Used |
|---|---|---|---|
| 1 | **ML (ONNX)** | 0.5–0.9 | Historical data + trained model available |
| 2 | **Historical Averages** | 0.3–0.7 | Historical data available, no model |
| 3 | **Cold-Start** | 0.1–0.3 | No history — uses user hints or defaults |

## Prerequisites

- **Python** >= 3.10
- **pip** packages (see `requirements.txt`)
- **PostgreSQL** (optional — for the feedback loop)

## Setup

```bash
cd inference-engine

# Create a virtual environment
python3 -m venv .venv
source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt
```

## Training the Model

Train the ONNX model on synthetic data (or real data from PostgreSQL):

```bash
# Train with 5000 synthetic samples (default)
python train_model.py --output model.onnx

# Train with more samples
python train_model.py --samples 10000 --output model.onnx
```

The trained model will be saved as `model.onnx`.

**Features** (input): `[image_hash, hist_avg_cpu, hist_avg_mem, hist_avg_duration]`
**Targets** (output): `[predicted_cpu, predicted_mem, predicted_duration]`

## Running

```bash
# Start the gRPC server (default port 50051)
python server.py

# With PostgreSQL (for the feedback loop)
DB_CONNECTION_STR="postgres://adaptive:adaptive-dev-password@localhost:5432/adaptiveworkflows?sslmode=disable" \
    python server.py

# Custom port
GRPC_PORT=9090 python server.py
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ONNX_MODEL_PATH` | `model.onnx` | Path to the trained ONNX model |
| `DB_CONNECTION_STR` | _(none)_ | PostgreSQL connection string |
| `GRPC_PORT` | `50051` | gRPC server port |

## Docker

Build and run using the Dockerfile (must be run from the project root):

```bash
# From the project root:
docker build -f inference-engine/Dockerfile -t inference-engine:latest .
docker run -p 50051:50051 inference-engine:latest
```

The Dockerfile automatically trains the model during build, so the ONNX model is baked into the image.

## Hosting

This service should be hosted as a **standalone container** accessible to the Kubernetes controller over gRPC on port **50051**.

### Option A: Docker Compose (Local Dev)
Already configured in the project's `docker-compose.yml`. Just run:
```bash
docker compose up inference-engine
```

### Option B: Kubernetes Deployment
Deploy as a separate pod/deployment in your cluster:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inference-engine
spec:
  replicas: 1
  selector:
    matchLabels:
      app: inference-engine
  template:
    metadata:
      labels:
        app: inference-engine
    spec:
      containers:
        - name: inference
          image: <your-registry>/inference-engine:latest
          ports:
            - containerPort: 50051
          env:
            - name: ONNX_MODEL_PATH
              value: /app/model.onnx
            - name: DB_CONNECTION_STR
              value: "postgres://adaptive:adaptive-dev-password@adaptive-workflows-postgres:5432/adaptiveworkflows?sslmode=disable"
            - name: GRPC_PORT
              value: "50051"
          resources:
            requests:
              cpu: 200m
              memory: 256Mi
            limits:
              cpu: "1"
              memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: inference-engine
spec:
  selector:
    app: inference-engine
  ports:
    - port: 50051
      targetPort: 50051
```

Then set the controller environment variable:
```bash
INFERENCE_GRPC_ADDR=inference-engine:50051
```

### Option C: Cloud Run / Fly.io / Any Container Host
Build the Docker image, push to a registry, and deploy. Expose port 50051.
Make sure PostgreSQL is accessible from the host for the feedback loop.

## Testing with grpcurl

```bash
# List services
grpcurl -plaintext localhost:50051 list

# Call Predict
grpcurl -plaintext -d '{
  "workflow_name": "test",
  "tasks": [
    {"name": "extract", "image": "python:3.12-slim"},
    {"name": "transform", "image": "python:3.12-slim"}
  ]
}' localhost:50051 adaptive.inference.v1.InferenceService/Predict
```

## Proto Definition

The service implements `InferenceService.Predict()` and `InferenceService.ReportMetrics()` from [`proto/inference.proto`](../proto/inference.proto).
