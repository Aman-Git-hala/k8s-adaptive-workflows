# Optimizer Service (C++)

A standalone C++ gRPC microservice that implements the **Greedy DAG Scheduler** for the K8s Adaptive Workflows project. It receives workflow DAG data, current task statuses, and ML predictions from the controller, then returns a schedule plan respecting resource constraints.

## Architecture

```
Controller (Go) ──gRPC:50052──▶ Optimizer Service (C++)
                                  │
                                  ├── Receives: DAG tasks, task statuses,
                                  │             ML predictions, resource limits
                                  │
                                  └── Returns:  Which tasks to start +
                                                resource allocations
```

## Algorithm

The **GreedyOptimizer** uses a resource-constrained greedy strategy:

1. **Find ready tasks**: Tasks whose dependencies have all `Succeeded`
2. **Resolve resources**: Use ML prediction → user hint → defaults (100m CPU, 128Mi Memory)
3. **Enforce constraints**: Only schedule if adding the task stays within `MaxResources`
4. **Return plan**: List of tasks to start with their CPU/Memory requests and limits

## Prerequisites

- **CMake** >= 3.20
- **gRPC** C++ libraries (`libgrpc++-dev`)
- **Protobuf** compiler and libraries (`protobuf-compiler`, `libprotobuf-dev`)
- **protoc-gen-grpc** (`protobuf-compiler-grpc`)

### Install on Ubuntu/Debian
```bash
sudo apt-get install build-essential cmake libgrpc++-dev \
    libprotobuf-dev protobuf-compiler protobuf-compiler-grpc
```

### Install on macOS
```bash
brew install cmake grpc protobuf
```

## Building

```bash
cd optimizer-service
mkdir -p build && cd build
cmake .. -DCMAKE_BUILD_TYPE=Release
make -j$(nproc)
```

The binary will be at `build/optimizer_server`.

## Running

```bash
# Default port (50052)
./build/optimizer_server

# Custom port
GRPC_PORT=9090 ./build/optimizer_server
```

## Docker

Build and run using the Dockerfile (must be run from the project root):

```bash
# From the project root:
docker build -f optimizer-service/Dockerfile -t optimizer-service:latest .
docker run -p 50052:50052 optimizer-service:latest
```

## Hosting

This service should be hosted as a **standalone container** accessible to the Kubernetes controller over gRPC on port **50052**.

### Option A: Docker Compose (Local Dev)
Already configured in the project's `docker-compose.yml`. Just run:
```bash
docker compose up optimizer
```

### Option B: Kubernetes Deployment
Deploy as a separate pod/deployment in your cluster:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: optimizer-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app: optimizer-service
  template:
    metadata:
      labels:
        app: optimizer-service
    spec:
      containers:
        - name: optimizer
          image: <your-registry>/optimizer-service:latest
          ports:
            - containerPort: 50052
          env:
            - name: GRPC_PORT
              value: "50052"
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 500m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: optimizer-service
spec:
  selector:
    app: optimizer-service
  ports:
    - port: 50052
      targetPort: 50052
```

Then set the controller environment variable:
```bash
OPTIMIZER_GRPC_ADDR=optimizer-service:50052
```

### Option C: Cloud Run / Fly.io / Any Container Host
Build the Docker image, push to a registry, and deploy. Expose port 50052.

## Testing with grpcurl

```bash
# List services
grpcurl -plaintext localhost:50052 list

# Call Plan
grpcurl -plaintext -d '{
  "workflow_name": "test",
  "optimization_goal": "MinimizeTime",
  "tasks": [
    {"name": "task-a", "image": "alpine:latest"},
    {"name": "task-b", "image": "alpine:latest", "dependencies": ["task-a"]}
  ],
  "task_statuses": {
    "task-a": {"phase": "Pending"},
    "task-b": {"phase": "Pending"}
  }
}' localhost:50052 adaptive.optimizer.v1.OptimizerService/Plan
```

## Proto Definition

The service implements `OptimizerService.Plan()` from [`proto/optimizer.proto`](../proto/optimizer.proto).
