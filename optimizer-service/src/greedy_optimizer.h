/*
 * greedy_optimizer.h — Resource-constrained greedy DAG scheduler.
 *
 * This is a C++ port of the Go GreedyOptimizer from
 * internal/optimizer/greedy.go. It implements the same algorithm:
 *
 *   1. Find all tasks whose dependencies have all Succeeded ("ready" tasks)
 *   2. For each ready task, check if adding it would exceed MaxResources
 *   3. If it fits, add it to the plan. If not, skip it for this cycle.
 *
 * Copyright 2026. Apache License 2.0.
 */

#pragma once

#include <cstdint>
#include <string>
#include <unordered_map>
#include <vector>

namespace adaptive {

// ─────────────────────────────────────────────────────────────
// Data types matching the protobuf messages
// ─────────────────────────────────────────────────────────────

struct ResourceHint {
    int64_t cpu_millicores = 0;
    int64_t memory_mib = 0;
};

struct DAGTask {
    std::string name;
    std::string image;
    std::vector<std::string> command;
    std::vector<std::string> dependencies;
    ResourceHint base_resources;
    bool has_base_resources = false;
};

struct TaskStatus {
    std::string phase;   // "Pending", "Running", "Succeeded", "Failed"
    std::string pod_name;
    int64_t allocated_cpu_millicores = 0;
    int64_t allocated_memory_mib = 0;
};

struct TaskPrediction {
    int64_t cpu_millicores = 0;
    int64_t memory_mib = 0;
    double estimated_duration_s = 0.0;
    double confidence = 0.0;
};

struct ResourceLimit {
    int64_t cpu_millicores = 0;
    int64_t memory_mib = 0;
};

struct TaskAllocation {
    std::string task_name;
    int64_t cpu_request_millicores = 0;
    int64_t memory_request_mib = 0;
    int64_t cpu_limit_millicores = 0;
    int64_t memory_limit_mib = 0;
};

struct PlanRequest {
    std::string workflow_name;
    std::string optimization_goal;
    std::vector<DAGTask> tasks;
    std::unordered_map<std::string, TaskStatus> task_statuses;
    std::unordered_map<std::string, TaskPrediction> predictions;
    ResourceLimit max_resources;
    bool has_max_resources = false;
};

struct PlanResponse {
    std::vector<TaskAllocation> tasks_to_start;
};

// ─────────────────────────────────────────────────────────────
// GreedyOptimizer
// ─────────────────────────────────────────────────────────────

class GreedyOptimizer {
public:
    GreedyOptimizer() = default;

    /// Plan decides which ready tasks to schedule, respecting MaxResources.
    PlanResponse Plan(const PlanRequest& request);

private:
    /// Default resource values when no prediction or hint is available.
    static constexpr int64_t kDefaultCpuMillicores = 100;
    static constexpr int64_t kDefaultMemoryMib = 128;
    static constexpr double kLimitMultiplier = 2.0;

    /// Resolve the resources for a single task.
    /// Priority: prediction > base_resources > defaults.
    TaskAllocation ResolveResources(
        const DAGTask& task,
        const std::unordered_map<std::string, TaskPrediction>& predictions
    );
};

}  // namespace adaptive
