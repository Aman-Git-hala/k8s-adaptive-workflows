/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package optimizer

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
)

// GreedyOptimizer is a simple, resource-constrained greedy scheduler.
// It finds all tasks whose dependencies are satisfied ("ready" tasks),
// then greedily schedules as many as possible without exceeding MaxResources.
type GreedyOptimizer struct{}

// NewGreedyOptimizer creates a new GreedyOptimizer.
func NewGreedyOptimizer() *GreedyOptimizer {
	return &GreedyOptimizer{}
}

// Plan implements Optimizer.Plan using a greedy strategy:
//  1. Find all tasks whose dependencies have all Succeeded.
//  2. For each ready task, check if adding it would exceed MaxResources.
//  3. If it fits, add it to the plan. If not, skip it for this cycle.
func (g *GreedyOptimizer) Plan(
	spec v1.AdaptiveWorkflowSpec,
	taskStatuses map[string]v1.TaskStatus,
	predictions Predictions,
) (SchedulePlan, error) {
	plan := SchedulePlan{}

	// Calculate current resource usage from running tasks.
	currentCPU := resource.Quantity{}
	currentMem := resource.Quantity{}
	for _, ts := range taskStatuses {
		if ts.Phase == v1.TaskPhaseRunning {
			if req, ok := ts.AllocatedResources.Requests[corev1.ResourceCPU]; ok {
				currentCPU.Add(req)
			}
			if req, ok := ts.AllocatedResources.Requests[corev1.ResourceMemory]; ok {
				currentMem.Add(req)
			}
		}
	}

	maxCPU := spec.MaxResources[corev1.ResourceCPU]
	maxMem := spec.MaxResources[corev1.ResourceMemory]

	for _, task := range spec.Tasks {
		// Skip tasks that are already started or finished.
		if ts, exists := taskStatuses[task.Name]; exists {
			if ts.Phase != v1.TaskPhasePending {
				continue
			}
		}

		// Check all dependencies are Succeeded.
		ready := true
		for _, dep := range task.Dependencies {
			depStatus, exists := taskStatuses[dep]
			if !exists || depStatus.Phase != v1.TaskPhaseSucceeded {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}

		// Determine resources for this task: use prediction if available,
		// otherwise fall back to the user's BaseResources hint,
		// otherwise use a minimal default.
		taskResources := g.resolveResources(task, predictions)

		// Check if adding this task would exceed MaxResources.
		taskCPU := taskResources.Requests[corev1.ResourceCPU]
		taskMem := taskResources.Requests[corev1.ResourceMemory]

		newCPU := currentCPU.DeepCopy()
		newCPU.Add(taskCPU)
		newMem := currentMem.DeepCopy()
		newMem.Add(taskMem)

		// If MaxResources is set, enforce the constraint.
		if !maxCPU.IsZero() && newCPU.Cmp(maxCPU) > 0 {
			continue // Would exceed CPU limit, skip for now.
		}
		if !maxMem.IsZero() && newMem.Cmp(maxMem) > 0 {
			continue // Would exceed Memory limit, skip for now.
		}

		// Schedule this task.
		plan.TasksToStart = append(plan.TasksToStart, TaskAllocation{
			TaskName:  task.Name,
			Resources: taskResources,
		})
		currentCPU = newCPU
		currentMem = newMem
	}

	return plan, nil
}

// resolveResources decides what resources to allocate for a task.
// Priority: Inference prediction > User's BaseResources > Minimal default.
func (g *GreedyOptimizer) resolveResources(
	task v1.TaskTemplate,
	predictions Predictions,
) corev1.ResourceRequirements {
	// 1. Use prediction if available.
	if pred, ok := predictions[task.Name]; ok {
		return pred.EstimatedResources
	}

	// 2. Use user-provided BaseResources if set.
	if len(task.BaseResources.Requests) > 0 || len(task.BaseResources.Limits) > 0 {
		return task.BaseResources
	}

	// 3. Fall back to a minimal default (100m CPU, 128Mi Memory).
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}
