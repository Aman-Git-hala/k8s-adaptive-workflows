FROM python:3.12-slim

LABEL maintainer="k8s-adaptive-workflows"
LABEL description="Inference Engine gRPC service with ONNX Runtime"

WORKDIR /app

# Install system dependencies for psycopg2 and general build.
RUN apt-get update && \
    apt-get install -y --no-install-recommends gcc libpq-dev && \
    rm -rf /var/lib/apt/lists/*

# Copy and install Python dependencies first (layer caching).
COPY inference-engine/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy proto stubs (context is project root, per docker-compose.yml).
COPY proto/gen/python/ /app/proto/gen/python/

# Copy application code.
COPY inference-engine/train_model.py inference-engine/server.py ./

# Train the model during build (so it's baked into the image).
RUN python train_model.py --output /app/model.onnx --samples 5000

# Runtime environment.
ENV ONNX_MODEL_PATH=/app/model.onnx
ENV GRPC_PORT=50051

EXPOSE 50051

CMD ["python", "server.py"]
