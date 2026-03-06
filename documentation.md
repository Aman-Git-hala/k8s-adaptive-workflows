# K8s Adaptive Workflows: Project Documentation

## 1. Project Overview
The `k8s-adaptive-workflows` project is a custom Kubernetes Operator built using Go and the Kubebuilder framework (`github.com/aman-githala/k8s-adaptive-workflows`).
The goal of the project is to provide a Kubernetes native way to manage "Adaptive Workflows" that can dynamically schedule, scale, and optimize workloads (represented as a Directed Acyclic Graph or DAG) across a cluster based on predictive models and workload characteristics.

**Current State**:
- We have scaffolded the base Kubernetes Operator project.
- The Custom Resource Definition (CRD) `AdaptiveWorkflow` has been initialized under the API Group `v1.wannabe.dev`.
- The reconciliation loop (`AdaptiveWorkflowReconciler`) has been scaffolded in `internal/controller/adaptiveworkflow_controller.go` but contains no custom logic yet.

---

## 2. High-Level Architecture
Based on our project requirements, the system is designed with several modular components interacting with a central Kubernetes controller.

1.  **User / CLI**
    - The entry point for users to submit workflow definitions (DAGs) and configuration.
    - Interfaces with the API Server.

2.  **API Server**
    - Receives requests from the User/CLI.
    - Responsible for taking user input and creating/updating the `AdaptiveWorkflow` Custom Resource (CR) in the Kubernetes API.

3.  **Main Controller (The Kubernetes Operator)**
    - The heart of the system.
    - Runs as a pod in the cluster, continuously watching for changes to `AdaptiveWorkflow` objects.
    - **Responsibilities**:
        - Parses the DAG structure defined in the `AdaptiveWorkflow` Spec.
        - Interfaces with the **State DB**, **Optimizer Core**, and **Inference Engine**.
        - Spawns and manages the underlying Kubernetes Pods in the Execution Plane.

4.  **Inference Engine**
    - A specialized component (likely an external service or a dedicated pod).
    - **Input**: Context (current cluster state, historical metrics).
    - **Output**: Predictions (e.g., expected task duration, resource requirements).
    - *How it fits*: The Main Controller queries the Inference Engine to make data-driven decisions before scheduling.

5.  **Optimizer Core**
    - A specialized component (likely an external service or library).
    - **Input**: DAG Structure and Context/Predictions.
    - **Output**: A Schedule Plan (which tasks should run next, on which nodes, and with what resources).
    - *How it fits*: The Main Controller consults the Optimizer to decide the most efficient way to execute the remaining graph.

6.  **State DB**
    - A persistent storage layer (e.g., Redis, PostgreSQL, or even Kubernetes custom resources/configmaps).
    - Maintains the intermediate state of workflows, logs, and metrics needed by the Inference Engine and Controller.

7.  **Kubernetes Cluster Execution Plane**
    - The standard Kubernetes nodes (Node A, Node B, Node C).
    - The Main Controller directly spawns pods on these nodes to execute the actual workflow tasks based on the Optimizer's plan.

---

## 3. The API Schema: `AdaptiveWorkflow`
To achieve smart and dynamic execution, the API is designed such that the user defines **what** to do and the **limits**, while the Optimizer and Inference Engine decide **how** and **when** to do it.

### 3.1 The Desired State (`AdaptiveWorkflowSpec`)
Users define the workflow in the `Spec`:
*   **Tasks**: A list of tasks (image, command) and their `Dependencies` (which other tasks must finish first) forming a DAG.
*   **MaxResources**: A global constraint defining the absolute maximum CPU/Memory the entire workflow can consume concurrently across the cluster.
*   **OptimizationGoal**: E.g., `"MinimizeTime"` (run tasks as fast as possible within limits) or `"MinimizeCost"` (pack tasks efficiently).

*(Implemented in `api/v1/adaptiveworkflow_types.go`)*

### 3.2 The Observed State (`AdaptiveWorkflowStatus`)
The Main Controller reports back the dynamic decisions and current execution phase:
*   **Phase**: The overall status (`Pending`, `Running`, `Completed`, `Failed`).
*   **TaskStatuses**: For each task in the DAG, the controller reports its individual phase, the Kubernetes Pod running it, and the exactly `AllocatedResources` chosen by the Inference/Optimizer engines.

---

## 4. Kubebuilder Project Structure Explained
For someone new to Kubebuilder, the generated file structure can be overwhelming. Here is a breakdown of the most critical files and directories we will be working with:

*   **`api/v1/`**: This directory contains the Go structs that define our Custom Resource Data (`AdaptiveWorkflow`). 
    *   `adaptiveworkflow_types.go`: This is where we defined the Spec and Status schema. When we change this, we string `make manifests` to generate the actual Kubernetes YAML files.
*   **`internal/controller/`**: This is where the core logic of the Operator lives.
    *   `adaptiveworkflow_controller.go`: This file contains the `Reconcile` function. This function is triggered automatically by Kubernetes every time an `AdaptiveWorkflow` is created, updated, or deleted. Our logic to talk to the Optimizer and spawn Pods goes here.
*   **`config/`**: Contains auto-generated YAML manifests used to deploy the CRDs (`config/crd`), Role-Based Access Control rules (`config/rbac`), and the controller itself (`config/manager`) to a cluster.
*   **`bin/`**: Contains binaries downloaded by Kubebuilder (like `controller-gen` used for generating manifests).
*   **`Makefile`**: Contains helpful commands like `make manifests` (generates CRD YAMLs from Go code), `make install` (installs CRDs to a cluster), and `make run` (runs the controller locally on your machine against your cluster).
