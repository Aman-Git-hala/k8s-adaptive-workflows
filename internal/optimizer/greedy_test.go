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

package optimizer_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
)

func allTaskNames(plan optimizer.SchedulePlan) []string {
	names := make([]string, 0, len(plan.TasksToStart))
	for _, a := range plan.TasksToStart {
		names = append(names, a.TaskName)
	}
	return names
}

func pendingStatus() v1.TaskStatus { return v1.TaskStatus{Phase: v1.TaskPhasePending} }
func runningStatus(cpu, mem string) v1.TaskStatus {
	return v1.TaskStatus{
		Phase: v1.TaskPhaseRunning,
		AllocatedResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(mem),
			},
		},
	}
}
func succeededStatus() v1.TaskStatus { return v1.TaskStatus{Phase: v1.TaskPhaseSucceeded} }

// TestGreedyOptimizer_RootTasksScheduled verifies that tasks with no dependencies
// are immediately eligible for scheduling.
func TestGreedyOptimizer_RootTasksScheduled(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"},
			{Name: "task-b", Image: "busybox"},
		},
	}
	statuses := map[string]v1.TaskStatus{
		"task-a": pendingStatus(),
		"task-b": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.TasksToStart) != 2 {
		t.Errorf("expected 2 tasks to start, got %d: %v", len(plan.TasksToStart), allTaskNames(plan))
	}
}

// TestGreedyOptimizer_DependencyBlocking verifies that a task with an unmet dependency
// is NOT included in the schedule plan.
func TestGreedyOptimizer_DependencyBlocking(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"},
			{Name: "task-b", Image: "busybox", Dependencies: []string{"task-a"}},
		},
	}
	// task-a is still Pending (not Succeeded), so task-b must NOT be scheduled.
	statuses := map[string]v1.TaskStatus{
		"task-a": pendingStatus(),
		"task-b": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	names := allTaskNames(plan)
	for _, n := range names {
		if n == "task-b" {
			t.Errorf("task-b should NOT be scheduled while task-a is still Pending")
		}
	}
}

// TestGreedyOptimizer_DependencyUnblocked verifies that task-b is scheduled once
// task-a has Succeeded.
func TestGreedyOptimizer_DependencyUnblocked(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"},
			{Name: "task-b", Image: "busybox", Dependencies: []string{"task-a"}},
		},
	}
	statuses := map[string]v1.TaskStatus{
		"task-a": succeededStatus(),
		"task-b": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	names := allTaskNames(plan)
	found := false
	for _, n := range names {
		if n == "task-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("task-b should be scheduled once task-a has Succeeded, got: %v", names)
	}
}

// TestGreedyOptimizer_SkipsAlreadyRunning verifies that Running tasks are not
// added to the plan again.
func TestGreedyOptimizer_SkipsAlreadyRunning(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"},
		},
	}
	statuses := map[string]v1.TaskStatus{
		"task-a": runningStatus("100m", "128Mi"),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.TasksToStart) != 0 {
		t.Errorf("expected 0 tasks to start (task-a already Running), got %d", len(plan.TasksToStart))
	}
}

// TestGreedyOptimizer_MaxCPUConstraint verifies that tasks are not scheduled
// when they would exceed the MaxResources CPU limit.
func TestGreedyOptimizer_MaxCPUConstraint(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	// Only 200m CPU allowed total; each task requests 200m.
	spec := v1.AdaptiveWorkflowSpec{
		MaxResources: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		},
		Tasks: []v1.TaskTemplate{
			{
				Name:  "task-a",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
				},
			},
			{
				Name:  "task-b",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
				},
			},
		},
	}
	statuses := map[string]v1.TaskStatus{
		"task-a": pendingStatus(),
		"task-b": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.TasksToStart) != 1 {
		t.Errorf("expected exactly 1 task due to CPU constraint, got %d: %v",
			len(plan.TasksToStart), allTaskNames(plan))
	}
}

// TestGreedyOptimizer_MaxMemoryConstraint verifies memory constraint enforcement.
func TestGreedyOptimizer_MaxMemoryConstraint(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		MaxResources: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Tasks: []v1.TaskTemplate{
			{
				Name:  "task-a",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
			},
			{
				Name:  "task-b",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
			},
		},
	}
	statuses := map[string]v1.TaskStatus{
		"task-a": pendingStatus(),
		"task-b": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.TasksToStart) != 1 {
		t.Errorf("expected exactly 1 task due to memory constraint, got %d: %v",
			len(plan.TasksToStart), allTaskNames(plan))
	}
}

// TestGreedyOptimizer_RunningTasksCountTowardLimit verifies that already-running
// tasks count towards the MaxResources budget.
func TestGreedyOptimizer_RunningTasksCountTowardLimit(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		MaxResources: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("300m"),
		},
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"},
			{
				Name:  "task-b",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")},
				},
			},
		},
	}
	// task-a is already running and consuming 200m CPU.
	statuses := map[string]v1.TaskStatus{
		"task-a": runningStatus("200m", "128Mi"),
		"task-b": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	// task-b requests 300m; with 200m already used (task-a) total = 500m > 300m → blocked.
	for _, alloc := range plan.TasksToStart {
		if alloc.TaskName == "task-b" {
			t.Errorf("task-b should be blocked because running tasks already use 200m of 300m budget")
		}
	}
}

// TestGreedyOptimizer_PredictionOverridesBaseResources verifies that an inference
// prediction takes priority over the task's BaseResources.
func TestGreedyOptimizer_PredictionOverridesBaseResources(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	predictedCPU := resource.MustParse("50m")
	predictedMem := resource.MustParse("64Mi")

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{
				Name:  "task-a",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"), // much higher base
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
			},
		},
	}
	statuses := map[string]v1.TaskStatus{"task-a": pendingStatus()}
	predictions := optimizer.Predictions{
		"task-a": {
			EstimatedResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    predictedCPU,
					corev1.ResourceMemory: predictedMem,
				},
			},
		},
	}

	plan, err := opt.Plan(spec, statuses, predictions)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.TasksToStart) != 1 {
		t.Fatalf("expected 1 task to start, got %d", len(plan.TasksToStart))
	}
	alloc := plan.TasksToStart[0]
	if alloc.Resources.Requests.Cpu().Cmp(predictedCPU) != 0 {
		t.Errorf("expected predicted CPU %v, got %v", predictedCPU.String(), alloc.Resources.Requests.Cpu().String())
	}
	if alloc.Resources.Requests.Memory().Cmp(predictedMem) != 0 {
		t.Errorf("expected predicted memory %v, got %v", predictedMem.String(), alloc.Resources.Requests.Memory().String())
	}
}

// TestGreedyOptimizer_DefaultResourcesWhenNoHint verifies that the optimizer falls
// back to the built-in defaults (100m CPU / 128Mi mem) when neither a prediction
// nor BaseResources is provided.
func TestGreedyOptimizer_DefaultResourcesWhenNoHint(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"}, // no BaseResources
		},
	}
	statuses := map[string]v1.TaskStatus{"task-a": pendingStatus()}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plan.TasksToStart) != 1 {
		t.Fatalf("expected 1 task to start, got %d", len(plan.TasksToStart))
	}
	alloc := plan.TasksToStart[0]
	defaultCPU := resource.MustParse("100m")
	defaultMem := resource.MustParse("128Mi")
	if alloc.Resources.Requests.Cpu().Cmp(defaultCPU) != 0 {
		t.Errorf("expected default CPU 100m, got %v", alloc.Resources.Requests.Cpu().String())
	}
	if alloc.Resources.Requests.Memory().Cmp(defaultMem) != 0 {
		t.Errorf("expected default memory 128Mi, got %v", alloc.Resources.Requests.Memory().String())
	}
}

// TestGreedyOptimizer_MultipleDependencies verifies that a task requiring multiple
// dependencies is only scheduled when ALL deps have Succeeded.
func TestGreedyOptimizer_MultipleDependencies(t *testing.T) {
	opt := optimizer.NewGreedyOptimizer()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"},
			{Name: "task-b", Image: "busybox"},
			{Name: "task-c", Image: "busybox", Dependencies: []string{"task-a", "task-b"}},
		},
	}

	// Only task-a succeeded.
	statuses := map[string]v1.TaskStatus{
		"task-a": succeededStatus(),
		"task-b": runningStatus("100m", "128Mi"),
		"task-c": pendingStatus(),
	}

	plan, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	for _, alloc := range plan.TasksToStart {
		if alloc.TaskName == "task-c" {
			t.Errorf("task-c should NOT be scheduled; task-b has not yet Succeeded")
		}
	}

	// Now both deps succeed.
	statuses["task-b"] = succeededStatus()
	plan2, err := opt.Plan(spec, statuses, optimizer.Predictions{})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	found := false
	for _, alloc := range plan2.TasksToStart {
		if alloc.TaskName == "task-c" {
			found = true
		}
	}
	if !found {
		t.Errorf("task-c should be scheduled once both task-a and task-b have Succeeded")
	}
}
