# K8s Adaptive Workflows: Project Timeline (March 6 - March 31, 2026)

Incremental development following the SRS multi-language architecture.
Each phase is a small, pushable chunk of work.

**Legend:**
- [x] Completed
- [/] In Progress
- [ ] Not Started

---

## Phase 0: Project Scaffolding & CRD Design (March 6 - 8)
*Set up the operator skeleton, define the API, and establish the pluggable interfaces.*

- [x] **March 6** — Kubebuilder scaffold, `AdaptiveWorkflow` CRD types (Spec/Status), `make manifests`, CI green.
- [x] **March 6** — Define `Optimizer` and `InferenceEngine` Go interfaces with initial stubs (`GreedyOptimizer`, `BaselineEngine`). Controller `.Owns(&corev1.Pod{})`.
- [x] **March 6** — `documentation.md` with architecture overview.
- [x] **March 8** — gRPC proto definitions (`proto/inference.proto`, `proto/optimizer.proto`). Generated Go stubs.

---

## Phase 1: Core Controller Reconcile Loop (March 9 - 12)
*Make the controller actually do something: read the DAG, call interfaces, spawn pods.*

- [x] **March 9** — Reconcile loop: fetch `AdaptiveWorkflow`, initialize `TaskStatuses` for new workflows, detect pod completions via owned Pods.
- [x] **March 10** — DAG execution: call `InferenceEngine.Predict()` → `Optimizer.Plan()` each cycle, create `corev1.Pod` with owner references, update `TaskStatuses` to `Running`.
- [x] **March 11** — Status lifecycle: update task phases as Pods transition (Running → Succeeded/Failed), compute `CurrentResourceUsage`, set overall `WorkflowPhase`.
- [x] **March 12** — Unit tests for DAG ordering, resource constraints, status transitions.

---

## Phase 2: PostgreSQL State DB (March 13 - 15)
*Add the State DB from the SRS diagram for storing historical execution metrics.*

- [x] **March 13** — PostgreSQL schema: `task_executions` table (task name, image, actual CPU/mem, duration, timestamp).
- [x] **March 14** — Go client package (`internal/statedb/`): connect to PostgreSQL, write execution metrics after pod completion.
- [x] **March 15** — K8s deployment manifests for PostgreSQL (lightweight, ~256MB RAM limit for local dev).

---

## Phase 3: Python Inference Engine (March 16 - 20)
*Build the ML inference service from the SRS: Python/ONNX, communicating over gRPC.*

- [x] **March 16** — Python project setup: `inference-engine/`, `requirements.txt`, generate Python gRPC stubs from proto.
- [x] **March 17** — Train a simple sklearn model (RandomForestRegressor) on synthetic data → export to ONNX. Features: task image hash, historical avg CPU/mem/duration. Targets: predicted CPU/mem/duration.
- [x] **March 18** — Implement `server.py`: gRPC server with `Predict()` (loads ONNX model, runs inference) and `ReportMetrics()` (stores feedback).
- [x] **March 19** — Cold-start fallback: if no history, use user's `BaseResources` hint or defaults. Test locally.
- [x] **March 20** — Dockerfile (ARM64/M1 compatible, lightweight base image).

---

## Phase 4: Optimizer gRPC Service (March 21 - 23)
*Wrap the Go optimizer as a standalone gRPC microservice matching the SRS.*

- [x] **March 21** — `cmd/optimizer/main.go`: gRPC server implementing `OptimizerService.Plan()` using existing `GreedyOptimizer` logic.
- [x] **March 22** — Optimizer Dockerfile. Test gRPC communication with the controller.
- [x] **March 23** — Wire the controller to call Inference + Optimizer via gRPC clients instead of direct Go calls. End-to-end test.

---

## Phase 5: `kwf` CLI & Docker Compose (March 24 - 27)
*Build the CLI binary from the SRS and a local dev setup that runs on M1/8GB.*

- [x] **March 24** — `cmd/kwf/main.go` with cobra: `kwf submit`, `kwf status`, `kwf list`.
- [x] **March 25** — `docker-compose.yml`: controller + inference-engine + optimizer + postgres, all resource-capped for 8GB RAM.
- [x] **March 26** — Test full flow locally: `kwf submit` → controller reconciles → calls inference (gRPC) → calls optimizer (gRPC) → spawns pods → writes metrics to PostgreSQL.
- [x] **March 27** — Error handling: pod failures, retry logic, workflow failure states.

---

## Phase 6: Polish, Testing & Release (March 28 - 31)
*Final integration, documentation, and submission.*

- [x] **March 28** — Integration tests: multi-branch DAGs, resource throttling, inference improving over repeated runs.
- [x] **March 29** — Kind/Minikube system test with a real cluster (50+ task workflow).
- [x] **March 30** — Update `README.md` (deployment instructions, architecture diagram). Sample YAML manifests in `config/samples/`.
- [x] **March 31** — Final cleanup, CI green, `git tag v1.0.0`, release notes. Submission.
