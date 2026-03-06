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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TaskTemplate defines a single logical unit of work in the DAG
type TaskTemplate struct {
	// Name of the task
	Name string `json:"name"`
	// Container Image to run
	Image string `json:"image"`
	// Command to run in the container
	// +optional
	Command []string `json:"command,omitempty"`
	// Dependencies lists the Names of other tasks that must complete before this one starts
	// +optional
	Dependencies []string `json:"dependencies,omitempty"`
	// Optional baseline resources if the user wants to provide a hint, though the Inference Engine may override this.
	// +optional
	BaseResources corev1.ResourceRequirements `json:"baseResources,omitempty"`
}

// AdaptiveWorkflowSpec defines the desired state of AdaptiveWorkflow
type AdaptiveWorkflowSpec struct {
	// Tasks is the list of all tasks comprising the DAG workflow
	Tasks []TaskTemplate `json:"tasks"`

	// MaxResources defines the absolute upper limit of cluster resources (CPU/Mem) this entire workflow can consume concurrently.
	// The Optimizer will ensure parallel tasks never exceed this aggregated limit.
	// +optional
	MaxResources corev1.ResourceList `json:"maxResources,omitempty"`

	// OptimizationGoal defines what the Optimizer should prioritize. e.g., "MinimizeTime" or "MinimizeCost"
	// +kubebuilder:default:="MinimizeTime"
	// +optional
	OptimizationGoal string `json:"optimizationGoal,omitempty"`
}

// TaskPhase tracks the observed state of a single task
type TaskPhase string

const (
	TaskPhasePending   TaskPhase = "Pending"
	TaskPhaseRunning   TaskPhase = "Running"
	TaskPhaseSucceeded TaskPhase = "Succeeded"
	TaskPhaseFailed    TaskPhase = "Failed"
)

// TaskStatus tracks the observed state of a single task
type TaskStatus struct {
	Phase TaskPhase `json:"phase"`
	// Reference to the actual Kubernetes Pod running this task
	// +optional
	PodName string `json:"podName,omitempty"`
	// The exact resources the Optimizer decided to allocate for this task based on Inference
	// +optional
	AllocatedResources corev1.ResourceRequirements `json:"allocatedResources,omitempty"`
}

// WorkflowPhase tracks the overall workflow execution
type WorkflowPhase string

const (
	WorkflowPhasePending   WorkflowPhase = "Pending"
	WorkflowPhaseRunning   WorkflowPhase = "Running"
	WorkflowPhaseCompleted WorkflowPhase = "Completed"
	WorkflowPhaseFailed    WorkflowPhase = "Failed"
)

// AdaptiveWorkflowStatus defines the observed state of AdaptiveWorkflow.
type AdaptiveWorkflowStatus struct {
	// Phase is the current high-level state of the workflow
	// +optional
	Phase WorkflowPhase `json:"phase,omitempty"`

	// TaskStatuses maps TaskName -> TaskStatus
	// +optional
	TaskStatuses map[string]TaskStatus `json:"taskStatuses,omitempty"`

	// Current concurrent resource usage of all running tasks
	// +optional
	CurrentResourceUsage corev1.ResourceList `json:"currentResourceUsage,omitempty"`

	// conditions represent the current state of the AdaptiveWorkflow resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AdaptiveWorkflow is the Schema for the adaptiveworkflows API
type AdaptiveWorkflow struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of AdaptiveWorkflow
	// +required
	Spec AdaptiveWorkflowSpec `json:"spec"`

	// status defines the observed state of AdaptiveWorkflow
	// +optional
	Status AdaptiveWorkflowStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// AdaptiveWorkflowList contains a list of AdaptiveWorkflow
type AdaptiveWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []AdaptiveWorkflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AdaptiveWorkflow{}, &AdaptiveWorkflowList{})
}
