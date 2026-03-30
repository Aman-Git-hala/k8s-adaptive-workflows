# K8s Adaptive Workflows: Project Documentation

## 1. Project Overview
The `k8s-adaptive-workflows` project is a custom Kubernetes Operator built using Go and the Kubebuilder framework (`github.com/aman-githala/k8s-adaptive-workflows`).
The goal of the project is to provide a Kubernetes native way to manage "Adaptive Workflows" that can dynamically schedule, scale, and optimize workloads (represented as a Directed Acyclic Graph or DAG) across a cluster based on predictive models and workload characteristics.

**Architecture**: Multi-language microservices communicating over gRPC:
- **Main Controller** (Go): Kubebuilder operator with reconcile loop, pod lifecycle
- **Inference Engine** (Python): ONNX-based ML predictions for task resources
- **Optimizer Service** (Go): Greedy DAG scheduler with resource constraints
- **State DB** (PostgreSQL): Historical execution metrics for ML feedback loop
- **kwf CLI** (Go): User-facing command-line tool

---

## 2. High-Level Architecture

```
User/CLI ──▶ Kubernetes API ──▶ Main Controller
                                     │
                    ┌────────────────┼────────────────┐
                    ▼                ▼                 ▼
             Inference Engine   Optimizer        State DB
             (Python/ONNX)     (Go/Greedy)      (PostgreSQL)
             gRPC :50051       gRPC :50052       TCP :5432
                    │                                 ▲
                    └─────── metrics feedback ────────┘
```

### 2.1 Main Controller (The Kubernetes Operator)
The heart of the system. Runs as a pod in the cluster, continuously watching for changes to `AdaptiveWorkflow` objects and the Pods it creates.

**Reconcile Loop** (`internal/controller/adaptiveworkflow_controller.go`):
1. **Fetch** the `AdaptiveWorkflow` CR from the API server
2. **Initialize** `TaskStatuses` map if new (all tasks → `Pending`)
3. **Handle deletion**: finalizer cleanup for external resources
4. **Sync pod state**: list owned Pods, update TaskStatuses (Running → Succeeded/Failed)
5. **Check terminal states**: Failed task → workflow Failed, all Succeeded → workflow Completed
6. **DAG execution**: call `Inference.Predict()` → `Optimizer.Plan()` → create Pods
7. **Compute resource usage**: sum allocations of all Running tasks
8. **Emit events**: Kubernetes events for task starts, completions, failures

### 2.2 Inference Engine (`inference-engine/`)
A Python gRPC service that predicts resource requirements for workflow tasks.

**Three prediction paths** (in priority order):
1. **ML prediction**: ONNX model trained on historical data (RandomForestRegressor)
2. **Historical averages**: raw database averages with 20% headroom  
3. **Cold-start fallback**: user's `BaseResources` hints or defaults (100m CPU, 128Mi)

**Feedback loop**: After each pod completes, the controller reports actual CPU/mem/duration back to the engine via `ReportMetrics()`. This data is stored in PostgreSQL and used to improve future predictions.

### 2.3 Optimizer Service (`cmd/optimizer/`)
A Go gRPC service wrapping the `GreedyOptimizer`.

**Algorithm**:
1. Find all tasks whose dependencies have all `Succeeded` ("ready" tasks)
2. For each ready task, determine resources (prediction > BaseResources > default)
3. Check if adding it would exceed `MaxResources`
4. If it fits, schedule it. If not, skip for this cycle.

### 2.4 State DB (`internal/statedb/`)
PostgreSQL database storing the `task_executions` table:
- workflow_name, task_name, image
- actual_cpu_m, actual_mem_mib, duration_s
- succeeded, completed_at

The Inference Engine queries 30-day rolling averages by image to make predictions.

### 2.5 kwf CLI (`cmd/kwf/`)
Cobra-based CLI with three commands:
- `kwf submit <file.yaml>` — Create or update an AdaptiveWorkflow
- `kwf status <name>` — Detailed task-level status with resource allocation
- `kwf list [-A]` — List all workflows (optionally across namespaces)

---

## 3. The API Schema: `AdaptiveWorkflow`

The API separates **user intent** from **system decisions**: users define *what* and *limits*, the system decides *how* and *when*.

### 3.1 The Desired State (`AdaptiveWorkflowSpec`)
- **Tasks**: DAG of tasks (image, command, dependencies, optional BaseResources)
- **MaxResources**: Global concurrent resource constraint (CPU/Memory)
- **OptimizationGoal**: `"MinimizeTime"` or `"MinimizeCost"` (default: MinimizeTime)

### 3.2 The Observed State (`AdaptiveWorkflowStatus`)
- **Phase**: Overall status (`Pending`, `Running`, `Completed`, `Failed`)
- **TaskStatuses**: Per-task phase, pod name, and allocated resources
- **CurrentResourceUsage**: Aggregate CPU/Memory of all running tasks
- **Conditions**: Standard Kubernetes conditions (Progressing, Available, Degraded)

*(Implemented in `api/v1/adaptiveworkflow_types.go`)*

---

## 4. Component Architecture

```
internal/
├── controller/
│   ├── adaptiveworkflow_controller.go      # Full reconcile loop
│   ├── adaptiveworkflow_controller_test.go  # Unit tests (Ginkgo/Gomega)
│   └── suite_test.go                       # Test suite setup
├── inference/
│   ├── inference.go      # Engine interface
│   └── baseline.go       # BaselineEngine (trusts user hints — in-process)
├── optimizer/
│   ├── optimizer.go       # Optimizer interface + shared types
│   └── greedy.go          # GreedyOptimizer (resource-constrained greedy)
├── grpcclient/
│   ├── inference_client.go  # gRPC client for remote Inference Engine
│   └── optimizer_client.go  # gRPC client for remote Optimizer
└── statedb/
    ├── client.go    # PostgreSQL + NoOp implementations
    └── schema.sql   # DDL for task_executions table
```

### Pluggability

The controller uses **Go interfaces** for both the Inference Engine and Optimizer:
- **Local mode** (default): in-process `BaselineEngine` + `GreedyOptimizer`
- **gRPC mode**: remote `InferenceClient` + `OptimizerClient`

Toggle via environment variables — no code changes needed.

---

## 5. gRPC Protocol

### Inference Service (`proto/inference.proto`)
- `Predict(PredictRequest) → PredictResponse` — per-task resource predictions
- `ReportMetrics(ReportMetricsRequest) → ReportMetricsResponse` — feedback loop

### Optimizer Service (`proto/optimizer.proto`)
- `Plan(PlanRequest) → PlanResponse` — which tasks to start + resource allocations

---

## 6. Deployment Options

### Local Dev (Docker Compose)
```bash
docker compose up -d       # PostgreSQL + Inference + Optimizer
make install               # Install CRDs
make run                   # Controller locally
```

### In-Cluster
```bash
make docker-build docker-push IMG=<registry>/<image>
make deploy IMG=<registry>/<image>
kubectl apply -k config/statedb/
```

### Kind Cluster Testing
```bash
kind create cluster --name adaptive-wf
make docker-build IMG=controller:test
kind load docker-image controller:test --name adaptive-wf
make deploy IMG=controller:test
kubectl apply -f config/samples/workflow-large.yaml
```

---

## 7. Testing

```bash
make test          # Unit tests (envtest: real K8s API + etcd)
make lint-fix      # Lint + auto-fix
go build ./...     # Build verification
```

Tests cover:
- Status initialization for new workflows
- DAG ordering (root tasks scheduled first)
- Diamond dependency handling
- Resource constraint enforcement (MaxResources)
- Pod lifecycle (Running → Succeeded/Failed)
