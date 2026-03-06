# K8s Adaptive Workflows: Project Documentation

## 1. Project Overview
The `k8s-adaptive-workflows` project is a custom Kubernetes Operator built using Go and the Kubebuilder framework (`github.com/aman-githala/k8s-adaptive-workflows`).
The goal of the project is to provide a Kubernetes native way to manage "Adaptive Workflows" that can dynamically schedule, scale, and optimize workloads (represented as a Directed Acyclic Graph or DAG) across a cluster based on predictive models and workload characteristics.

**Current State**:
- The Custom Resource Definition (CRD) `AdaptiveWorkflow` is defined under the API Group `v1.wannabe.dev`.
- The Optimizer and Inference Engine are defined as **pluggable Go interfaces** with initial implementations.
- The controller is wired to watch both `AdaptiveWorkflow` objects and the Pods it creates.
- Kubernetes itself (via the CRD Status subresource) is used as the state store — no external database needed.

---

## 2. High-Level Architecture
The system has several modular components interacting with a central Kubernetes controller.

1.  **User / CLI**
    - The entry point for users to submit workflow definitions (DAGs) and configuration.
    - Users write an `AdaptiveWorkflow` YAML manifest and apply it via `kubectl`.

2.  **Main Controller (The Kubernetes Operator)**
    - The heart of the system. Runs as a pod in the cluster, continuously watching for changes to `AdaptiveWorkflow` objects **and** the Pods it creates.
    - **Responsibilities**:
        - Parses the DAG structure defined in the `AdaptiveWorkflow` Spec.
        - Calls the **Inference Engine** for predictions.
        - Calls the **Optimizer** for a schedule plan.
        - Spawns and manages the underlying Kubernetes Pods in the Execution Plane.
        - Tracks all state in the CRD `Status` subresource (no external DB needed).

3.  **Inference Engine** (`internal/inference/`)
    - A **Go interface** (`Engine`) that predicts resource needs and duration for each task.
    - **Current implementation**: `BaselineEngine` — trusts the user's `BaseResources` hints as-is. Returns empty predictions for tasks without hints.
    - **Future**: Can be swapped out for an ML-based engine that uses historical execution data.

4.  **Optimizer** (`internal/optimizer/`)
    - A **Go interface** (`Optimizer`) that decides which tasks to start and with what resources.
    - **Current implementation**: `GreedyOptimizer` — finds DAG-ready tasks (all dependencies succeeded), then greedily schedules as many as possible without exceeding `MaxResources`.
    - **Resource resolution priority**: Inference prediction > User's `BaseResources` hint > Minimal default (100m CPU, 128Mi Mem).
    - **Future**: Can be swapped for more advanced algorithms (ILP solver, simulated annealing, etc.).

5.  **Kubernetes Cluster Execution Plane**
    - The standard Kubernetes nodes. The Main Controller spawns pods on these nodes using owner references, so the controller is automatically notified when pods complete or fail.

> **Design Decision: No External State DB.** Kubernetes etcd (via the CRD Status subresource) serves as the state store. The `AdaptiveWorkflowStatus` tracks workflow phase, per-task status, pod names, allocated resources, and current resource usage. This avoids operational overhead of deploying and managing an external database.

---

## 3. The API Schema: `AdaptiveWorkflow`
The API separates **user intent** from **system decisions**: users define *what* and *limits*, the system decides *how* and *when*.

### 3.1 The Desired State (`AdaptiveWorkflowSpec`)
Users define the workflow in the `Spec`:
*   **Tasks**: A list of tasks (image, command) and their `Dependencies` (which other tasks must finish first) forming a DAG.
*   **MaxResources**: A global constraint defining the absolute maximum CPU/Memory the entire workflow can consume concurrently across the cluster.
*   **OptimizationGoal**: E.g., `"MinimizeTime"` (run tasks as fast as possible within limits) or `"MinimizeCost"` (pack tasks efficiently).

### 3.2 The Observed State (`AdaptiveWorkflowStatus`)
The Main Controller reports back the dynamic decisions and current execution phase:
*   **Phase**: The overall status (`Pending`, `Running`, `Completed`, `Failed`).
*   **TaskStatuses**: For each task in the DAG, the controller reports its individual phase, the Kubernetes Pod running it, and the `AllocatedResources` chosen by the Inference/Optimizer engines.
*   **CurrentResourceUsage**: The aggregate CPU/Memory currently being consumed by all running tasks.

*(Implemented in `api/v1/adaptiveworkflow_types.go`)*

---

## 4. Pluggable Component Architecture

The Optimizer and Inference Engine are **Go interfaces**, making them easy to swap or extend.

```
internal/
├── inference/
│   ├── inference.go      # Engine interface
│   └── baseline.go       # BaselineEngine (trusts user hints)
├── optimizer/
│   ├── optimizer.go       # Optimizer interface + shared types
│   └── greedy.go          # GreedyOptimizer (resource-constrained greedy)
└── controller/
    ├── adaptiveworkflow_controller.go      # Reconcile loop
    └── adaptiveworkflow_controller_test.go # Unit tests
```

---

## 5. Kubebuilder Project Structure
*   **`api/v1/`**: Go structs defining the CRD schema. Run `make manifests` after changes.
*   **`internal/controller/`**: The `Reconcile` function, triggered on every `AdaptiveWorkflow` or owned Pod change.
*   **`internal/optimizer/`**: Optimizer interface and implementations.
*   **`internal/inference/`**: Inference Engine interface and implementations.
*   **`config/`**: Auto-generated YAML manifests (CRDs, RBAC, manager deployment).
*   **`Makefile`**: `make manifests`, `make test`, `make run`, `make install`, etc.
