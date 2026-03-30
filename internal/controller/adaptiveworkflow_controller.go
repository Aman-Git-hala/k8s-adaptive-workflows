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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/inference"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
)

const (
	// workflowFinalizer is used for cleanup logic.
	workflowFinalizer = "v1.wannabe.dev/finalizer"

	// requeueInterval is used when the workflow is still running.
	requeueInterval = 10 * time.Second
)

// AdaptiveWorkflowReconciler reconciles a AdaptiveWorkflow object
type AdaptiveWorkflowReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Inference inference.Engine
	Optimizer optimizer.Optimizer
	Recorder  record.EventRecorder
}

// +kubebuilder:rbac:groups=v1.wannabe.dev,resources=adaptiveworkflows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=v1.wannabe.dev,resources=adaptiveworkflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=v1.wannabe.dev,resources=adaptiveworkflows/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop. It reads the AdaptiveWorkflow CR,
// detects pod state changes, calls Inference + Optimizer, spawns new pods, and
// updates the CR status to reflect reality.
func (r *AdaptiveWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// ──────────────────────────────────────────────────────────────────
	// 1. Fetch the AdaptiveWorkflow CR
	// ──────────────────────────────────────────────────────────────────
	wf := &v1.AdaptiveWorkflow{}
	if err := r.Get(ctx, req.NamespacedName, wf); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AdaptiveWorkflow resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get AdaptiveWorkflow: %w", err)
	}

	// ──────────────────────────────────────────────────────────────────
	// 2. Handle deletion / finalizer
	// ──────────────────────────────────────────────────────────────────
	if !wf.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(wf, workflowFinalizer) {
			log.Info("Running finalizer cleanup for workflow", "name", wf.Name)
			// Future: clean up external resources (state DB entries, etc.)
			controllerutil.RemoveFinalizer(wf, workflowFinalizer)
			if err := r.Update(ctx, wf); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(wf, workflowFinalizer) {
		controllerutil.AddFinalizer(wf, workflowFinalizer)
		if err := r.Update(ctx, wf); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ──────────────────────────────────────────────────────────────────
	// 3. Initialize TaskStatuses for new workflows
	// ──────────────────────────────────────────────────────────────────
	if wf.Status.TaskStatuses == nil {
		wf.Status.TaskStatuses = make(map[string]v1.TaskStatus, len(wf.Spec.Tasks))
		for _, task := range wf.Spec.Tasks {
			wf.Status.TaskStatuses[task.Name] = v1.TaskStatus{
				Phase: v1.TaskPhasePending,
			}
		}
		wf.Status.Phase = v1.WorkflowPhasePending
		meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
			Type:    "Progressing",
			Status:  metav1.ConditionTrue,
			Reason:  "Initialized",
			Message: fmt.Sprintf("Workflow initialized with %d tasks", len(wf.Spec.Tasks)),
		})
		if err := r.Status().Update(ctx, wf); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to initialize status: %w", err)
		}
		r.Recorder.Event(wf, corev1.EventTypeNormal, "Initialized",
			fmt.Sprintf("Workflow initialized with %d tasks", len(wf.Spec.Tasks)))
		return ctrl.Result{Requeue: true}, nil
	}

	// ──────────────────────────────────────────────────────────────────
	// 4. If workflow is already terminal, do nothing
	// ──────────────────────────────────────────────────────────────────
	if wf.Status.Phase == v1.WorkflowPhaseCompleted || wf.Status.Phase == v1.WorkflowPhaseFailed {
		return ctrl.Result{}, nil
	}

	// ──────────────────────────────────────────────────────────────────
	// 5. Detect pod completions — sync actual pod state into TaskStatuses
	// ──────────────────────────────────────────────────────────────────
	if err := r.syncPodsToStatus(ctx, wf); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to sync pod status: %w", err)
	}

	// ──────────────────────────────────────────────────────────────────
	// 6. Check for workflow-level failure (any Failed task → workflow Failed)
	// ──────────────────────────────────────────────────────────────────
	for taskName, ts := range wf.Status.TaskStatuses {
		if ts.Phase == v1.TaskPhaseFailed {
			wf.Status.Phase = v1.WorkflowPhaseFailed
			meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "TaskFailed",
				Message: fmt.Sprintf("Task %q failed", taskName),
			})
			meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
				Type:    "Progressing",
				Status:  metav1.ConditionFalse,
				Reason:  "TaskFailed",
				Message: fmt.Sprintf("Workflow stopped because task %q failed", taskName),
			})
			if err := r.Status().Update(ctx, wf); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(wf, corev1.EventTypeWarning, "WorkflowFailed",
				fmt.Sprintf("Workflow failed because task %q failed", taskName))
			return ctrl.Result{}, nil
		}
	}

	// ──────────────────────────────────────────────────────────────────
	// 7. Check if all tasks are Succeeded → workflow Completed
	// ──────────────────────────────────────────────────────────────────
	allSucceeded := true
	for _, ts := range wf.Status.TaskStatuses {
		if ts.Phase != v1.TaskPhaseSucceeded {
			allSucceeded = false
			break
		}
	}
	if allSucceeded && len(wf.Status.TaskStatuses) > 0 {
		wf.Status.Phase = v1.WorkflowPhaseCompleted
		wf.Status.CurrentResourceUsage = nil
		meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "AllTasksSucceeded",
			Message: "All workflow tasks completed successfully",
		})
		meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
			Type:    "Progressing",
			Status:  metav1.ConditionFalse,
			Reason:  "Completed",
			Message: "Workflow execution is complete",
		})
		if err := r.Status().Update(ctx, wf); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(wf, corev1.EventTypeNormal, "WorkflowCompleted",
			"All tasks completed successfully")
		return ctrl.Result{}, nil
	}

	// ──────────────────────────────────────────────────────────────────
	// 8. DAG execution: Inference → Optimizer → spawn pods
	// ──────────────────────────────────────────────────────────────────
	wf.Status.Phase = v1.WorkflowPhaseRunning

	// 8a. Call Inference Engine for predictions.
	predictions, err := r.Inference.Predict(wf.Spec)
	if err != nil {
		log.Error(err, "inference engine failed, proceeding with empty predictions")
		predictions = optimizer.Predictions{}
	}

	// 8b. Call Optimizer for a schedule plan.
	plan, err := r.Optimizer.Plan(wf.Spec, wf.Status.TaskStatuses, predictions)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("optimizer failed: %w", err)
	}

	// 8c. Spawn pods for each task in the plan.
	for _, alloc := range plan.TasksToStart {
		if err := r.createTaskPod(ctx, wf, alloc); err != nil {
			log.Error(err, "failed to create pod for task", "task", alloc.TaskName)
			r.Recorder.Event(wf, corev1.EventTypeWarning, "PodCreationFailed",
				fmt.Sprintf("Failed to create pod for task %q: %v", alloc.TaskName, err))
			continue
		}
		// Update task status to Running.
		ts := wf.Status.TaskStatuses[alloc.TaskName]
		ts.Phase = v1.TaskPhaseRunning
		ts.AllocatedResources = alloc.Resources
		ts.PodName = podName(wf.Name, alloc.TaskName)
		wf.Status.TaskStatuses[alloc.TaskName] = ts

		r.Recorder.Event(wf, corev1.EventTypeNormal, "TaskStarted",
			fmt.Sprintf("Started task %q with pod %q", alloc.TaskName, ts.PodName))
		log.Info("Created pod for task", "task", alloc.TaskName, "pod", ts.PodName)
	}

	// ──────────────────────────────────────────────────────────────────
	// 9. Compute CurrentResourceUsage
	// ──────────────────────────────────────────────────────────────────
	wf.Status.CurrentResourceUsage = r.computeResourceUsage(wf.Status.TaskStatuses)

	// ──────────────────────────────────────────────────────────────────
	// 10. Update status + requeue
	// ──────────────────────────────────────────────────────────────────
	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
		Type:    "Progressing",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciling",
		Message: fmt.Sprintf("%d tasks running", countPhase(wf.Status.TaskStatuses, v1.TaskPhaseRunning)),
	})
	if err := r.Status().Update(ctx, wf); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// ════════════════════════════════════════════════════════════════════
// Helper methods
// ════════════════════════════════════════════════════════════════════

// syncPodsToStatus lists all owned pods and updates TaskStatuses based on pod phase.
func (r *AdaptiveWorkflowReconciler) syncPodsToStatus(ctx context.Context, wf *v1.AdaptiveWorkflow) error {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(wf.Namespace),
		client.MatchingFields{"metadata.ownerReferences.uid": string(wf.UID)},
	); err != nil {
		// If the field index isn't set up, fall back to label-based listing.
		if err := r.List(ctx, podList,
			client.InNamespace(wf.Namespace),
			client.MatchingLabels{"adaptive-workflow": wf.Name},
		); err != nil {
			return err
		}
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		taskName := pod.Labels["task-name"]
		if taskName == "" {
			continue
		}

		ts, exists := wf.Status.TaskStatuses[taskName]
		if !exists {
			continue
		}

		// Only update if the task is in Running phase (we care about completions).
		if ts.Phase != v1.TaskPhaseRunning {
			continue
		}

		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			ts.Phase = v1.TaskPhaseSucceeded
			wf.Status.TaskStatuses[taskName] = ts
			r.Recorder.Event(wf, corev1.EventTypeNormal, "TaskSucceeded",
				fmt.Sprintf("Task %q completed successfully", taskName))
		case corev1.PodFailed:
			ts.Phase = v1.TaskPhaseFailed
			wf.Status.TaskStatuses[taskName] = ts
			r.Recorder.Event(wf, corev1.EventTypeWarning, "TaskFailed",
				fmt.Sprintf("Task %q failed", taskName))
		}
		// PodRunning and PodPending → no change needed.
	}

	return nil
}

// createTaskPod creates a Kubernetes Pod for the given task allocation.
func (r *AdaptiveWorkflowReconciler) createTaskPod(
	ctx context.Context,
	wf *v1.AdaptiveWorkflow,
	alloc optimizer.TaskAllocation,
) error {
	// Find the task template.
	var task *v1.TaskTemplate
	for i := range wf.Spec.Tasks {
		if wf.Spec.Tasks[i].Name == alloc.TaskName {
			task = &wf.Spec.Tasks[i]
			break
		}
	}
	if task == nil {
		return fmt.Errorf("task %q not found in spec", alloc.TaskName)
	}

	name := podName(wf.Name, alloc.TaskName)

	// Check if pod already exists.
	existingPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: wf.Namespace}, existingPod); err == nil {
		return nil // Pod already exists, skip creation.
	}

	command := task.Command
	if len(command) == 0 {
		command = []string{"echo", fmt.Sprintf("Running task %s", task.Name)}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: wf.Namespace,
			Labels: map[string]string{
				"adaptive-workflow":            wf.Name,
				"task-name":                    alloc.TaskName,
				"app.kubernetes.io/managed-by": "k8s-adaptive-workflows",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:      "task",
					Image:     task.Image,
					Command:   command,
					Resources: alloc.Resources,
				},
			},
		},
	}

	// Set owner reference so pods are garbage collected when the workflow is deleted.
	if err := controllerutil.SetControllerReference(wf, pod, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	if err := r.Create(ctx, pod); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create pod %s: %w", name, err)
	}

	return nil
}

// computeResourceUsage sums the allocated resources for all Running tasks.
func (r *AdaptiveWorkflowReconciler) computeResourceUsage(
	taskStatuses map[string]v1.TaskStatus,
) corev1.ResourceList {
	totalCPU := resource.Quantity{}
	totalMem := resource.Quantity{}

	for _, ts := range taskStatuses {
		if ts.Phase != v1.TaskPhaseRunning {
			continue
		}
		if req, ok := ts.AllocatedResources.Requests[corev1.ResourceCPU]; ok {
			totalCPU.Add(req)
		}
		if req, ok := ts.AllocatedResources.Requests[corev1.ResourceMemory]; ok {
			totalMem.Add(req)
		}
	}

	if totalCPU.IsZero() && totalMem.IsZero() {
		return nil
	}

	return corev1.ResourceList{
		corev1.ResourceCPU:    totalCPU,
		corev1.ResourceMemory: totalMem,
	}
}

// countPhase counts tasks in a given phase.
func countPhase(statuses map[string]v1.TaskStatus, phase v1.TaskPhase) int {
	count := 0
	for _, ts := range statuses {
		if ts.Phase == phase {
			count++
		}
	}
	return count
}

// podName generates a deterministic pod name for a task within a workflow.
// Names are lowercased and sanitized to be valid RFC 1123 subdomain names.
func podName(workflowName, taskName string) string {
	name := fmt.Sprintf("%s-%s", workflowName, taskName)
	return strings.ToLower(name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AdaptiveWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.AdaptiveWorkflow{}).
		Owns(&corev1.Pod{}).
		Named("adaptiveworkflow").
		Complete(r)
}
