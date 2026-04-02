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

// Package optimizer defines the interface and implementations for workflow
// scheduling. The Optimizer receives a DAG of tasks, predictions from the
// Inference Engine, and resource constraints, and produces a SchedulePlan
// that tells the controller which tasks to run next and with what resources.
package optimizer

import (
	corev1 "k8s.io/api/core/v1"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
)

// ============================================================================
// Input Types (Predictions from Inference Engine)
// ============================================================================

// TaskPrediction holds a single task's predicted resource needs and duration.
type TaskPrediction struct {
	// EstimatedDuration is the predicted wall-clock run time in seconds.
	EstimatedDuration float64
	// EstimatedResources are the predicted CPU/Memory needs.
	EstimatedResources corev1.ResourceRequirements
}

// Predictions holds per-task predictions provided by the Inference Engine.
// The Optimizer uses these to make informed scheduling decisions.
type Predictions map[string]TaskPrediction

// ============================================================================
// Output Types (Optimizer Decision)
// ============================================================================

// TaskAllocation describes the resources the optimizer has decided
// to assign to a single task.
type TaskAllocation struct {
	// TaskName is the name of the task from the DAG.
	TaskName string
	// Resources are the exact CPU/Memory requests and limits to set on the Pod.
	Resources corev1.ResourceRequirements
}

// SchedulePlan is the output of the Optimizer: a set of tasks that should
// be started now, along with their allocated resources.
type SchedulePlan struct {
	// TasksToStart lists the tasks that should be launched in this
	// reconciliation cycle, each with its decided resource allocation.
	TasksToStart []TaskAllocation
}

// ============================================================================
// Optimizer Interface
// ============================================================================

// Optimizer decides which ready tasks to schedule and with what resources,
// given the current state of the workflow, predictions, and constraints.
type Optimizer interface {
	// Plan analyzes the workflow state and returns a SchedulePlan indicating
	// which tasks should be started in the current reconciliation cycle with
	// their respective resource allocations.
	//
	// Parameters:
	//   - spec: The complete workflow specification
	//   - currentTaskStatuses: Map of task names to their current execution status
	//   - inferencePredictions: Predictions from the Inference Engine for resource needs
	//
	// Returns:
	//   - SchedulePlan: The tasks to start and their resource allocations
	//   - error: Any error encountered during planning
	Plan(
		spec v1.AdaptiveWorkflowSpec,
		currentTaskStatuses map[string]v1.TaskStatus,
		inferencePredictions Predictions,
	) (SchedulePlan, error)
}
