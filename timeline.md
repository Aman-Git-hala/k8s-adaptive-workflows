# K8s Adaptive Workflows: Project Timeline (March 6 - March 31, 2026)

This timeline follows a structured Software Engineering approach (inspired by the V-Model of Systems Engineering) to break down the development of the Kubernetes Operator into manageable, versioned phases.

**Legend:**
- [x] Completed
- [/] In Progress
- [ ] Not Started

---

## Phase 0: Project Inception & Architecture Setup (March 6 - March 8)
*The foundation phase where we define the system boundaries, architecture, and the API surface the user will interact with.*

- [x] **March 6 - System Design (The Architecture)**: Analyze the requirements and design the high-level architecture involving the User, API Server, Main Controller, Inference Engine, Optimizer Core, State DB, and Execution Plane. Documented in `documentation.md`.
- [x] **March 6 - Module Design (The API Schema)**: Define the core `AdaptiveWorkflow` Custom Resource (CRD).
  - Defined the Spec (`Tasks`, `MaxResources`, `OptimizationGoal`) to map the DAG and constraints.
  - Defined the Status (`Phase`, `TaskStatuses`, `AllocatedResources`) to map the execution state.
- [x] **March 6 - Scaffolding & CI Integration**: Generate the initial Go structs using Kubebuilder (`make manifests`), fix unit testing for the new CRD validation, and ensure GitHub Actions (Lint, Tests) are green.

---

## Phase 1: v0.1 Core Controller & Base Execution (March 9 - March 15)
*This phase focuses strictly on making the Main Controller read a DAG and spawn basic Kubernetes Pods, without any advanced optimization yet.*

- [ ] **March 9-10 - State DB Integration**: Connect the Go controller to a persistence layer (e.g., Redis or an internal Kubernetes ConfigMap) to store intermediate workflow states, logs, and metrics so the controller does not lose track if it restarts.
- [ ] **March 11-12 - Core Reconciliation Loop (Coding)**: Write the main logic in `internal/controller/adaptiveworkflow_controller.go`. The controller must watch for new `AdaptiveWorkflow` objects, parse the dependencies (`Dependencies` field), and figure out which tasks have no unfinished dependencies (i.e., the "Ready" tasks).
- [ ] **March 13-14 - Pod Execution Plane**: Write the Kubernetes client logic to literally spawn `corev1.Pod` objects for those "Ready" tasks using the `Image` and `Command` provided by the user. Update the CRD `Status` to reflect `Pending` or `Running`.
- [ ] **March 15 - v0.1 Unit Testing**: (V-Model: Unit Testing). Write Go tests to verify that if a DAG `A -> B` is submitted, the controller spawns Pod `A`, waits for it to succeed, and only then spawns Pod `B`.

---

## Phase 2: v0.2 Optimization & Inference Integration (March 16 - March 24)
*This is where the "Smart" part of the operator is built. We integrate the Inference Engine to predict task behavior and the Optimizer to schedule them efficiently.*

- [ ] **March 16-18 - Inference Engine API Interface**: Create a mock interface/service for the Inference Engine. The controller will send the DAG structure and request predictions (e.g., "How long will Task A take?" or "How much Memory will Task B need?").
- [ ] **March 19-21 - Optimizer Core Integration**: Implement the scheduling logic. The Optimizer takes the Inference predictions and the user's `MaxResources` constraint and decides the exact `AllocatedResources` for each Pod. If running Task A and B together exceeds `MaxResources`, the Optimizer must delay B even if its dependencies are met.
- [ ] **March 22-24 - v0.2 Integration Testing**: (V-Model: Integration Testing). Test the entire flow. Submit a complex workflow, ensure the Controller asks the Inference Engine, gets a schedule from the Optimizer, and spawns the Pods with exactly those `AllocatedResources`.

---

## Phase 3: v1.0.0 Resilience, System Validation & Release (March 25 - March 31)
*The final polish. Handling errors gracefully, stress-testing on a real cluster, and preparing the project for public release.*

- [ ] **March 25-27 - Error Handling & Retries**: What happens if a Pod crashes? What if it runs Out of Memory (OOM)? The controller must detect these failures, update the `TaskStatus` to `Failed`, and either retry the task (if configured) or fail the entire `AdaptiveWorkflow`.
- [ ] **March 28-29 - System Testing**: (V-Model: System Testing). Deploy the full Operator to a local Kubernetes cluster (like Kind or Minikube). Submit huge workflows (e.g., 50 parallel tasks) to ensure the Optimizer successfully throttles execution to stay beneath `MaxResources`.
- [ ] **March 30 - Documentation & Examples**: Write clear deployment instructions in the `README.md`. Create a `config/samples/` directory with example YAML files so new users can easily test the workflows.
- [ ] **March 31 - Final Release (v1.0.0)**: (V-Model: Acceptance Testing). Final code cleanup, ensure CI/CD is perfect, tag the repository on GitHub (`git tag v1.0.0`), and write the release notes.
