# Project Overview: K8s Adaptive Workflows

At its core, **K8s Adaptive Workflows** is an intelligent Kubernetes operator designed to run dependent tasks (a workflow/DAG) as standard Pods while dynamically optimizing the resources they consume. 

Here is what happens when you submit a workflow:

1. **The Controller (Go):** The `AdaptiveWorkflowReconciler` detects a new workflow containing a series of tasks. It parses the dependencies.
2. **Inference Engine (Python via gRPC):** Before creating task pods, our Reconciler hits the Inference Engine. The engine uses historical data from PostgreSQL to **predict** the CPU/Memory requests and expected duration of those tasks.
3. **Optimizer Service (C++ via gRPC):** Armed with predictions, the Reconciler asks the Optimizer how to schedule the tasks effectively based on available resources. The Optimizer generates a precise `SchedulePlan`.
4. **Execution:** The Reconciler spawns Kubernetes `Pods` based on the optimal plan.
5. **Feedback Loop (StateDB):** When task Pods succeed or fail, the Reconciler records the actual CPU/Memory utilized and duration in **PostgreSQL**. The Inference Engine queries this data to make smarter predictions for the next workflow run!

---

# Local Setup Guide (Optimized for 8GB Mac M1)

Because you are on an M1 Air with 8GB RAM, memory limits are very tight. We leverage **Docker Compose** to run our microservices very cheaply, **Kind** (Kubernetes IN Docker) for the lightest local cluster, and run the controller locally outside of Docker so you don't need to mount massive images.

## Step 1: Install Prerequisites

First, ensure Docker Desktop (or OrbStack, which uses less RAM on M1 Macs) is running.
Then quickly install `kind`:

```bash
brew install kind
```

## Step 2: Initialize Docker-Compose Services

Your `docker-compose.yml` is already heavily optimized to cap at around ~800MB RAM combined. It launches the PostgreSQL State DB, the Python Inference Engine, and the C++ Optimizer as lightweight native-ARM containers.

1. Open your terminal in the project root: `cd ~/k8s-adaptive-workflows`
2. Spin up the backend dependencies:
   ```bash
   docker compose up -d --build
   ```
3. Check they are running with `docker compose ps`. You should see `inference-engine` (on port 50051), `optimizer` (on port 50052), and `postgres` (on port 5432).

## Step 3: Create the Kind Cluster

We need a lightweight Kubernetes cluster to run your custom pods. `kind` spins this up inside a standard Docker container.

```bash
kind create cluster --name adaptive-test
```
> [!TIP]
> Wait a minute for the cluster components to report as Ready. To verify, run `kubectl get nodes`. It should show `adaptive-test-control-plane`.

## Step 4: Install Your Custom CRDs 

Before starting the operator, your local Kind cluster needs to know what an `AdaptiveWorkflow` Custom Resource is. We tell your Makefile to generate these files and inject them.

```bash
make manifests
make install
```

## Step 5: Start the Controller Locally

To save us from having to bake and load the heavy Manager Go binary into the Kind image, we run it locally. It will automatically connect to your active `kind` context, and can also reach `localhost:50051` to talk to your dockerized microservices.

```bash
# This binds up this terminal window streaming logs of the controller
make run
```

## Step 6: Submit a Workflow!

With everything running, open a **brand new split terminal**.
Let's deploy a test workflow sample built to test standard small usage:

```bash
kubectl apply -f config/samples/v1_v1_adaptiveworkflow.yaml
```

To really stress-test the optimizer algorithm against your laptop's constraints, we've provided a massive 50+ node DAG template you can run:

```bash
kubectl apply -f config/samples/workflow-large.yaml
```

**What you will see:**
1. Back on your controller logs (`make run`), you will see the prediction logs happening via hitting `localhost:50051`.
2. The controller will ask the Kind cluster to create pods.
3. Run `kubectl get pods` in your new terminal and you will see the tasks actively running and succeeding!
