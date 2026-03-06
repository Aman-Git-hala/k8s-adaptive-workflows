# K8s Adaptive Workflows: Project Timeline (March 6 - March 31, 2026)

This timeline follows a structured Software Engineering approach (inspired by the V-Model) to break down the development into manageable, versioned phases.

**Legend:**
- [x] Completed
- [/] In Progress
- [ ] Not Started

---

## Phase 0: Project Inception & Architecture Setup (March 6 - March 8)
*Define the system boundaries, architecture, API surface, and pluggable component interfaces.*

- [x] **March 6 - System Design (The Architecture)**: Design the high-level architecture (Main Controller, Inference Engine, Optimizer Core, Execution Plane). Documented in `documentation.md`.
- [x] **March 6 - Module Design (The API Schema)**: Define the `AdaptiveWorkflow` CRD (Spec: `Tasks`, `MaxResources`, `OptimizationGoal`; Status: `Phase`, `TaskStatuses`, `AllocatedResources`).
- [x] **March 6 - Scaffolding & CI**: Generate Go structs via Kubebuilder (`make manifests`), fix unit tests, ensure GitHub Actions green.
- [x] **March 6 - Architecture Fixes**: Define `Optimizer` and `InferenceEngine` as pluggable Go interfaces with initial implementations (`GreedyOptimizer`, `BaselineEngine`). Fix controller to `.Owns(&corev1.Pod{})`. Remove State DB (use K8s Status instead).

---

## Phase 1: v0.1 Core Controller & Base Execution (March 9 - March 15)
*Make the Main Controller read a DAG, call the Optimizer/Inference interfaces, and spawn Kubernetes Pods.*

- [ ] **March 9-10 - Core Reconciliation Loop**: Write the main `Reconcile()` logic: fetch the `AdaptiveWorkflow`, initialize `TaskStatuses` for new workflows, detect pod completions/failures via owned Pods.
- [ ] **March 11-12 - DAG Execution & Pod Spawning**: Call the `InferenceEngine.Predict()` then `Optimizer.Plan()` each cycle to get `TasksToStart`. Create `corev1.Pod` objects with owner references, update `TaskStatuses` to `Running`.
- [ ] **March 13-14 - Status Management**: Implement full lifecycle tracking: update individual `TaskStatus` phases as Pods transition, compute `CurrentResourceUsage`, set overall `WorkflowPhase` to `Completed` or `Failed`.
- [ ] **March 15 - v0.1 Unit Testing**: (V-Model: Unit Testing). Write Go tests verifying DAG ordering (A→B spawns A first), resource constraint enforcement (tasks delayed when MaxResources exceeded), and status transitions.

---

## Phase 2: v0.2 Smarter Optimization & Inference (March 16 - March 24)
*Upgrade the "Smart" behavior: better predictions and more sophisticated scheduling.*

- [ ] **March 16-18 - Historical Inference Engine**: Implement a second `Engine` that records past task execution data (duration, actual resource usage) and uses it to predict future runs. Store historical data in a ConfigMap or annotation.
- [ ] **March 19-21 - Advanced Optimizer**: Implement a second `Optimizer` using priority-based scheduling (e.g., Critical Path Method — prioritize tasks on the longest path through the DAG to minimize total makespan).
- [ ] **March 22-24 - v0.2 Integration Testing**: (V-Model: Integration Testing). Test with complex multi-branch DAGs. Verify the Optimizer correctly throttles parallel execution within `MaxResources` and the Inference Engine improves predictions over multiple runs.

---

## Phase 3: v1.0.0 Resilience, System Validation & Release (March 25 - March 31)
*Error handling, stress-testing on a real cluster, and project release.*

- [ ] **March 25-27 - Error Handling & Retries**: Detect Pod failures (CrashLoopBackOff, OOMKilled), update `TaskStatus` to `Failed`, implement configurable retry logic, and fail the entire workflow if retries are exhausted.
- [ ] **March 28-29 - System Testing**: (V-Model: System Testing). Deploy to a local Kind/Minikube cluster. Submit large workflows (50+ tasks) and verify the Optimizer correctly manages concurrency within resource constraints.
- [ ] **March 30 - Documentation & Examples**: Write deployment instructions in `README.md`. Create sample YAML manifests in `config/samples/` demonstrating various workflow patterns (linear, fan-out, diamond).
- [ ] **March 31 - Final Release (v1.0.0)**: (V-Model: Acceptance Testing). Final cleanup, CI/CD verification, `git tag v1.0.0`, release notes.
