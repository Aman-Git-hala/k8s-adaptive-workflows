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

package inference_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/inference"
)

// TestBaselineEngine_ReturnsBaseResourcesAsPrediction verifies that a task with
// BaseResources set gets those resources returned as a prediction.
func TestBaselineEngine_ReturnsBaseResourcesAsPrediction(t *testing.T) {
	engine := inference.NewBaselineEngine()

	cpu := resource.MustParse("200m")
	mem := resource.MustParse("256Mi")

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{
				Name:  "task-a",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    cpu,
						corev1.ResourceMemory: mem,
					},
				},
			},
		},
	}

	predictions, err := engine.Predict(spec)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	pred, ok := predictions["task-a"]
	if !ok {
		t.Fatal("expected prediction for task-a, got none")
	}
	if pred.EstimatedResources.Requests.Cpu().Cmp(cpu) != 0 {
		t.Errorf("expected CPU %v, got %v", cpu.String(), pred.EstimatedResources.Requests.Cpu().String())
	}
	if pred.EstimatedResources.Requests.Memory().Cmp(mem) != 0 {
		t.Errorf("expected memory %v, got %v", mem.String(), pred.EstimatedResources.Requests.Memory().String())
	}
}

// TestBaselineEngine_NoPredictionForTaskWithoutBaseResources verifies that tasks
// without BaseResources are omitted from predictions (the optimizer uses defaults).
func TestBaselineEngine_NoPredictionForTaskWithoutBaseResources(t *testing.T) {
	engine := inference.NewBaselineEngine()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{Name: "task-a", Image: "busybox"}, // no BaseResources
		},
	}

	predictions, err := engine.Predict(spec)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	if _, ok := predictions["task-a"]; ok {
		t.Errorf("expected no prediction for task-a (no BaseResources), but got one")
	}
}

// TestBaselineEngine_MultipleTasksMixed verifies that only tasks with BaseResources
// get predictions, while others are omitted.
func TestBaselineEngine_MultipleTasksMixed(t *testing.T) {
	engine := inference.NewBaselineEngine()

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{
				Name:  "task-with-hint",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
				},
			},
			{
				Name:  "task-no-hint",
				Image: "busybox",
			},
		},
	}

	predictions, err := engine.Predict(spec)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	if _, ok := predictions["task-with-hint"]; !ok {
		t.Error("expected prediction for task-with-hint")
	}
	if _, ok := predictions["task-no-hint"]; ok {
		t.Error("expected NO prediction for task-no-hint (no BaseResources)")
	}
}

// TestBaselineEngine_EmptyWorkflow verifies that an empty workflow returns no predictions.
func TestBaselineEngine_EmptyWorkflow(t *testing.T) {
	engine := inference.NewBaselineEngine()

	spec := v1.AdaptiveWorkflowSpec{Tasks: []v1.TaskTemplate{}}

	predictions, err := engine.Predict(spec)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	if len(predictions) != 0 {
		t.Errorf("expected 0 predictions for empty workflow, got %d", len(predictions))
	}
}

// TestBaselineEngine_LimitsPreserved verifies that Limits (not just Requests) in
// BaseResources are carried through to predictions unchanged.
func TestBaselineEngine_LimitsPreserved(t *testing.T) {
	engine := inference.NewBaselineEngine()

	cpuLimit := resource.MustParse("400m")
	memLimit := resource.MustParse("512Mi")

	spec := v1.AdaptiveWorkflowSpec{
		Tasks: []v1.TaskTemplate{
			{
				Name:  "task-a",
				Image: "busybox",
				BaseResources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    cpuLimit,
						corev1.ResourceMemory: memLimit,
					},
				},
			},
		},
	}

	predictions, err := engine.Predict(spec)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	pred, ok := predictions["task-a"]
	if !ok {
		t.Fatal("expected prediction for task-a")
	}
	if pred.EstimatedResources.Limits.Cpu().Cmp(cpuLimit) != 0 {
		t.Errorf("expected CPU limit %v, got %v",
			cpuLimit.String(), pred.EstimatedResources.Limits.Cpu().String())
	}
	if pred.EstimatedResources.Limits.Memory().Cmp(memLimit) != 0 {
		t.Errorf("expected memory limit %v, got %v",
			memLimit.String(), pred.EstimatedResources.Limits.Memory().String())
	}
}
