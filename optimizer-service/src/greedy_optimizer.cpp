/*
 * greedy_optimizer.cpp — Resource-constrained greedy DAG scheduler.
 *
 * C++ implementation of the greedy algorithm originally written in Go
 * (internal/optimizer/greedy.go). The scheduling logic is identical:
 *
 * For each task in DAG order:
 *   1. Skip if not Pending
 *   2. Check all dependencies are Succeeded
 *   3. Resolve resources (prediction > base hint > default)
 *   4. Check if adding it would exceed MaxResources
 *   5. If it fits, schedule it
 *
 * Copyright 2026. Apache License 2.0.
 */

#include "greedy_optimizer.h"

#include <algorithm>

namespace adaptive {

PlanResponse GreedyOptimizer::Plan(const PlanRequest& request) {
    PlanResponse response;

    // Calculate current resource usage from running tasks.
    int64_t current_cpu = 0;
    int64_t current_mem = 0;

    for (const auto& [name, status] : request.task_statuses) {
        if (status.phase == "Running") {
            current_cpu += status.allocated_cpu_millicores;
            current_mem += status.allocated_memory_mib;
        }
    }

    const int64_t max_cpu = request.has_max_resources
                                ? request.max_resources.cpu_millicores
                                : 0;
    const int64_t max_mem = request.has_max_resources
                                ? request.max_resources.memory_mib
                                : 0;

    // Iterate through tasks in DAG order (spec order).
    for (const auto& task : request.tasks) {
        // Skip tasks that are already started or finished.
        auto status_it = request.task_statuses.find(task.name);
        if (status_it != request.task_statuses.end()) {
            if (status_it->second.phase != "Pending") {
                continue;
            }
        }

        // Check all dependencies are Succeeded.
        bool ready = true;
        for (const auto& dep : task.dependencies) {
            auto dep_it = request.task_statuses.find(dep);
            if (dep_it == request.task_statuses.end() ||
                dep_it->second.phase != "Succeeded") {
                ready = false;
                break;
            }
        }
        if (!ready) {
            continue;
        }

        // Resolve resources for this task.
        TaskAllocation alloc = ResolveResources(task, request.predictions);
        alloc.task_name = task.name;

        // Check if adding this task would exceed MaxResources.
        int64_t new_cpu = current_cpu + alloc.cpu_request_millicores;
        int64_t new_mem = current_mem + alloc.memory_request_mib;

        if (max_cpu > 0 && new_cpu > max_cpu) {
            continue;  // Would exceed CPU limit, skip for now.
        }
        if (max_mem > 0 && new_mem > max_mem) {
            continue;  // Would exceed Memory limit, skip for now.
        }

        // Schedule this task.
        response.tasks_to_start.push_back(alloc);
        current_cpu = new_cpu;
        current_mem = new_mem;
    }

    return response;
}

TaskAllocation GreedyOptimizer::ResolveResources(
    const DAGTask& task,
    const std::unordered_map<std::string, TaskPrediction>& predictions) {
    TaskAllocation alloc;

    // 1. Use prediction if available.
    auto pred_it = predictions.find(task.name);
    if (pred_it != predictions.end()) {
        const auto& pred = pred_it->second;
        alloc.cpu_request_millicores = pred.cpu_millicores;
        alloc.memory_request_mib = pred.memory_mib;
        alloc.cpu_limit_millicores =
            static_cast<int64_t>(pred.cpu_millicores * 1.5);
        alloc.memory_limit_mib =
            static_cast<int64_t>(pred.memory_mib * 1.5);
        return alloc;
    }

    // 2. Use user-provided BaseResources if set.
    if (task.has_base_resources &&
        (task.base_resources.cpu_millicores > 0 ||
         task.base_resources.memory_mib > 0)) {
        alloc.cpu_request_millicores = task.base_resources.cpu_millicores;
        alloc.memory_request_mib = task.base_resources.memory_mib;
        alloc.cpu_limit_millicores =
            static_cast<int64_t>(task.base_resources.cpu_millicores *
                                 kLimitMultiplier);
        alloc.memory_limit_mib =
            static_cast<int64_t>(task.base_resources.memory_mib *
                                 kLimitMultiplier);
        return alloc;
    }

    // 3. Fall back to minimal defaults (100m CPU, 128Mi Memory).
    alloc.cpu_request_millicores = kDefaultCpuMillicores;
    alloc.memory_request_mib = kDefaultMemoryMib;
    alloc.cpu_limit_millicores =
        static_cast<int64_t>(kDefaultCpuMillicores * kLimitMultiplier);
    alloc.memory_limit_mib =
        static_cast<int64_t>(kDefaultMemoryMib * kLimitMultiplier);
    return alloc;
}

}  // namespace adaptive
