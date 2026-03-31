# K8s Adaptive Workflows

A Kubernetes Operator that dynamically schedules, scales, and optimizes DAG-based workflows using ML-driven resource predictions. Built with Go, Kubebuilder, Python/ONNX, and gRPC.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         User / kwf CLI                              │
│   kwf submit workflow.yaml    kwf status my-wf    kwf list          │
└────────────────────────────────┬────────────────────────────────────┘
                                 │ kubectl / REST API
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    Main Controller (Go Operator)                     │
│                                                                      │
│  ┌──────────────┐   ┌──────────────────┐   ┌───────────────────┐    │
│  │  Reconcile   │──▶│ Inference Engine  │──▶│    Optimizer       │   │
│  │  Loop        │   │ (gRPC :50051)     │   │ (gRPC :50052)      │   │
│  │              │◀──│ Python + ONNX     │◀──│ Go + Greedy Algo   │   │
│  └──────┬───────┘   └────────┬─────────┘   └───────────────────┘    │
│         │                    │                                       │
│         │ Create/Watch Pods  │ Read/Write Metrics                    │
│         ▼                    ▼                                       │
│  ┌──────────────┐   ┌──────────────────┐                            │
│  │  K8s Pods    │   │  PostgreSQL       │                           │
│  │  (Task Exec) │   │  State DB         │                           │
│  └──────────────┘   └──────────────────┘                            │
└─────────────────────────────────────────────────────────────────────┘
```

## Components

| Component | Language | Port | Description |
|---|---|---|---|
| **Main Controller** | Go | — | Kubebuilder operator, reconcile loop, pod lifecycle |
| **Inference Engine** | Python | 50051 | ONNX-based ML predictions for task resources ([docs](inference-engine/README.md)) |
| **Optimizer Service** | C++ | 50052 | Greedy DAG scheduler with resource constraints ([docs](optimizer-service/README.md)) |
| **State DB** | PostgreSQL | 5432 | Historical execution metrics for ML training |
| **kwf CLI** | Go | — | Submit, status, list workflows |

## Quick Start

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- A Kubernetes cluster (Kind, Minikube, or remote)
- kubectl configured

### Option 1: Local Development (Docker Compose + `make run`)

This is the fastest way to get started. The gRPC services and database run in Docker; the controller runs directly on your machine.

```bash
# 1. Start supporting services (PostgreSQL, Inference Engine, Optimizer)
docker compose up -d

# 2. Install CRDs into your cluster
make install

# 3. Run the controller locally (connects to your current kubeconfig context)
make run

# 4. In another terminal, submit a workflow
kubectl apply -f config/samples/v1_v1_adaptiveworkflow.yaml

# 5. Watch the workflow progress
kubectl get adaptiveworkflows -w
kubectl get pods -l adaptive-workflow=etl-pipeline -w
```

### Option 2: Full In-Cluster Deployment

```bash
# 1. Build and push images
export IMG=your-registry/k8s-adaptive-workflows:latest
make docker-build docker-push IMG=$IMG

# 2. Deploy the controller + CRDs + RBAC
make deploy IMG=$IMG

# 3. Deploy PostgreSQL state DB
kubectl apply -k config/statedb/

# 4. Submit a workflow
kubectl apply -f config/samples/v1_v1_adaptiveworkflow.yaml
```

### Option 3: Using the `kwf` CLI

```bash
# Build the CLI
go build -o bin/kwf ./cmd/kwf/

# Submit a workflow
./bin/kwf submit config/samples/v1_v1_adaptiveworkflow.yaml

# Check status
./bin/kwf status etl-pipeline

# List all workflows
./bin/kwf list
```

### Option 4: Local Kubernetes (Docker-Native with Kind)

This is the best way to run a real Kubernetes environment using only Docker.

```bash
# 1. Create a local Kind cluster
kind create cluster --name k8s-adaptive-workflows

# 2. Build local images
make docker-build IMG=controller:latest
docker build -t inference-engine:latest -f inference-engine/Dockerfile .
docker build -t optimizer-service:latest -f optimizer-service/Dockerfile .

# 3. Load local images into the Kind cluster (No need to push to GitHub/Registry!)
kind load docker-image controller:latest --name k8s-adaptive-workflows
kind load docker-image inference-engine:latest --name k8s-adaptive-workflows
kind load docker-image optimizer-service:latest --name k8s-adaptive-workflows

# 4. Deploy everything (Controller + Optimizer + Inference + DB + CRDs)
make deploy IMG=controller:latest

# 5. Submit the sample workflow
kubectl apply -f config/samples/v1_v1_adaptiveworkflow.yaml
```

## Example Workflow (ETL Pipeline)

```yaml
apiVersion: v1.wannabe.dev/v1
kind: AdaptiveWorkflow
metadata:
  name: etl-pipeline
spec:
  optimizationGoal: MinimizeTime
  maxResources:
    cpu: "2"
    memory: "1Gi"
  tasks:
    - name: extract-users
      image: python:3.12-slim
      command: ["python", "-c", "print('Extracting...')"]

    - name: transform-users
      image: python:3.12-slim
      command: ["python", "-c", "print('Transforming...')"]
      dependencies: ["extract-users"]

    - name: load-warehouse
      image: python:3.12-slim
      command: ["python", "-c", "print('Loading...')"]
      dependencies: ["transform-users"]
```

The controller will:
1. **Predict** resource needs via the Inference Engine (ONNX model or cold-start defaults)
2. **Schedule** ready tasks via the Optimizer (greedy DAG scheduler with resource constraints)
3. **Spawn** Kubernetes Pods with optimized resource requests/limits
4. **Learn** from actual execution metrics to improve future predictions

## How the DAG Execution Works

```
         extract-users    extract-orders
              │                │
              ▼                ▼
        transform-users  transform-orders
              │                │
              └────────┬───────┘
                       ▼
                   join-data
                       │
                       ▼
                load-warehouse
                       │
                       ▼
              send-notification
```

1. **Root tasks** (no dependencies) are scheduled first (`extract-users`, `extract-orders`)
2. When a task's **pod succeeds**, its dependents become eligible
3. The **Optimizer** checks MaxResources — only starts new tasks if there's headroom
4. The **Inference Engine** predicts how much CPU/memory each task actually needs
5. After completion, **actual metrics** are fed back to PostgreSQL for future learning

## Project Structure

For detailed file-by-file explanations, see [DOCUMENTATION.md](DOCUMENTATION.md).

```
├── api/v1/                    # CRD types (AdaptiveWorkflow)
├── cmd/
│   ├── main.go                # Controller entry point
│   └── kwf/main.go            # CLI tool
├── config/
│   ├── crd/                   # Generated CRDs (DO NOT EDIT)
│   ├── samples/               # Example workflows
│   └── statedb/               # PostgreSQL deployment manifests
├── inference-engine/          # Python inference gRPC service (Standalone)
│   ├── server.py              # gRPC server (ONNX + PostgreSQL)
│   ├── train_model.py         # Model training script
│   └── Dockerfile
├── optimizer-service/         # C++ optimizer gRPC service (Standalone)
│   ├── src/                   # C++ sources for greedy algorithm
│   ├── CMakeLists.txt         # Build config
│   └── Dockerfile
├── internal/
│   ├── controller/            # Go reconcile loop for AdaptiveWorkflow
│   ├── inference/             # Go inference interface + local fallback
│   ├── optimizer/             # Go optimizer interface + local fallback
│   ├── grpcclient/            # gRPC client wrappers for Go controller
│   └── statedb/               # PostgreSQL client for feedback loop
├── proto/                     # gRPC protobuf definitions
│   ├── inference.proto
│   ├── optimizer.proto
│   └── gen/                   # Generated Go + C++ + Python stubs
├── docker-compose.yml         # Local dev stack (Postgres + Inference + Optimizer)
└── Makefile                   # Build/test/deploy commands
```

## Development

```bash
# Run tests
make test

# Regenerate CRDs after editing types
make manifests generate

# Build everything
go build ./...

# Lint
make lint-fix
```

## License

Apache License 2.0
