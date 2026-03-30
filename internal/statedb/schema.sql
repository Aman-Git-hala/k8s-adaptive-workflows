-- K8s Adaptive Workflows: State DB Schema
-- PostgreSQL schema for storing historical execution metrics.
-- Used by the Inference Engine to learn from past workflow runs.

CREATE TABLE IF NOT EXISTS task_executions (
    id              BIGSERIAL PRIMARY KEY,
    workflow_name   TEXT        NOT NULL,
    task_name       TEXT        NOT NULL,
    image           TEXT        NOT NULL,
    actual_cpu_m    BIGINT      NOT NULL,  -- Actual CPU usage in millicores
    actual_mem_mib  BIGINT      NOT NULL,  -- Actual memory usage in MiB
    duration_s      DOUBLE PRECISION NOT NULL,  -- Wall-clock duration in seconds
    succeeded       BOOLEAN     NOT NULL DEFAULT TRUE,
    completed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for fast lookups by image (used by inference engine).
CREATE INDEX IF NOT EXISTS idx_task_executions_image
    ON task_executions (image);

-- Index for time-based queries (recent metrics are more relevant).
CREATE INDEX IF NOT EXISTS idx_task_executions_completed
    ON task_executions (completed_at DESC);

-- Composite index for per-workflow task lookups.
CREATE INDEX IF NOT EXISTS idx_task_executions_workflow_task
    ON task_executions (workflow_name, task_name);
