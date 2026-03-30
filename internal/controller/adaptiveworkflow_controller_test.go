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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/inference"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
)

// safeDeleteWorkflow removes the finalizer and deletes the workflow.
func safeDeleteWorkflow(ctx context.Context, nn types.NamespacedName) {
	wf := &v1.AdaptiveWorkflow{}
	if err := k8sClient.Get(ctx, nn, wf); err != nil {
		return // Already deleted
	}
	// Remove finalizer so deletion can proceed.
	if controllerutil.ContainsFinalizer(wf, workflowFinalizer) {
		controllerutil.RemoveFinalizer(wf, workflowFinalizer)
		_ = k8sClient.Update(ctx, wf)
	}
	// Delete owned pods.
	podList := &corev1.PodList{}
	_ = k8sClient.List(ctx, podList)
	for i := range podList.Items {
		p := &podList.Items[i]
		if p.Labels["adaptive-workflow"] == nn.Name {
			_ = k8sClient.Delete(ctx, p)
		}
	}
	// Delete workflow.
	_ = k8sClient.Delete(ctx, wf)
}

// reconcileN runs the reconciler N times, returning the last result.
func reconcileN(ctx context.Context, r *AdaptiveWorkflowReconciler, nn types.NamespacedName, n int) (reconcile.Result, error) {
	var result reconcile.Result
	var err error
	for i := 0; i < n; i++ {
		result, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		if err != nil {
			return result, err
		}
	}
	return result, err
}

func newReconciler() *AdaptiveWorkflowReconciler {
	return &AdaptiveWorkflowReconciler{
		Client:    k8sClient,
		Scheme:    k8sClient.Scheme(),
		Inference: inference.NewBaselineEngine(),
		Optimizer: optimizer.NewGreedyOptimizer(),
		Recorder:  record.NewFakeRecorder(100),
	}
}

var _ = Describe("AdaptiveWorkflow Controller", func() {
	Context("Status initialization and scheduling", func() {
		const resourceName = "test-init"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			wf := &v1.AdaptiveWorkflow{}
			err := k8sClient.Get(ctx, nn, wf)
			if err != nil && errors.IsNotFound(err) {
				res := &v1.AdaptiveWorkflow{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: v1.AdaptiveWorkflowSpec{
						Tasks: []v1.TaskTemplate{
							{Name: "extract", Image: "alpine:latest", Command: []string{"echo", "extracting"}},
							{Name: "transform", Image: "alpine:latest", Command: []string{"echo", "transforming"}, Dependencies: []string{"extract"}},
							{Name: "load", Image: "alpine:latest", Command: []string{"echo", "loading"}, Dependencies: []string{"transform"}},
						},
						MaxResources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				}
				Expect(k8sClient.Create(ctx, res)).To(Succeed())
			}
		})

		AfterEach(func() {
			safeDeleteWorkflow(ctx, nn)
		})

		It("should schedule root tasks and respect DAG ordering", func() {
			r := newReconciler()

			// Reconcile until scheduling happens (finalizer → init → schedule).
			_, err := reconcileN(ctx, r, nn, 3)
			Expect(err).NotTo(HaveOccurred())

			wf := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, nn, wf)).To(Succeed())

			// "extract" has no deps → should be Running.
			Expect(wf.Status.TaskStatuses["extract"].Phase).To(Equal(v1.TaskPhaseRunning))
			// "transform" depends on "extract" → still Pending.
			Expect(wf.Status.TaskStatuses["transform"].Phase).To(Equal(v1.TaskPhasePending))
			// "load" depends on "transform" → still Pending.
			Expect(wf.Status.TaskStatuses["load"].Phase).To(Equal(v1.TaskPhasePending))
			// Workflow should be Running.
			Expect(wf.Status.Phase).To(Equal(v1.WorkflowPhaseRunning))

			// Pod should have been created.
			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-init-extract", Namespace: "default",
			}, pod)).To(Succeed())
			Expect(pod.Spec.Containers[0].Image).To(Equal("alpine:latest"))
		})
	})

	Context("Diamond DAG ordering", func() {
		const resourceName = "diamond-wf"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			wf := &v1.AdaptiveWorkflow{}
			err := k8sClient.Get(ctx, nn, wf)
			if err != nil && errors.IsNotFound(err) {
				res := &v1.AdaptiveWorkflow{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: v1.AdaptiveWorkflowSpec{
						Tasks: []v1.TaskTemplate{
							{Name: "a", Image: "alpine:latest", Command: []string{"echo", "a"}},
							{Name: "b", Image: "alpine:latest", Command: []string{"echo", "b"}, Dependencies: []string{"a"}},
							{Name: "c", Image: "alpine:latest", Command: []string{"echo", "c"}, Dependencies: []string{"a"}},
							{Name: "d", Image: "alpine:latest", Command: []string{"echo", "d"}, Dependencies: []string{"b", "c"}},
						},
						MaxResources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				}
				Expect(k8sClient.Create(ctx, res)).To(Succeed())
			}
		})

		AfterEach(func() {
			safeDeleteWorkflow(ctx, nn)
		})

		It("should only schedule root task 'a' first", func() {
			r := newReconciler()

			_, err := reconcileN(ctx, r, nn, 3)
			Expect(err).NotTo(HaveOccurred())

			wf := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, nn, wf)).To(Succeed())
			Expect(wf.Status.TaskStatuses["a"].Phase).To(Equal(v1.TaskPhaseRunning))
			Expect(wf.Status.TaskStatuses["b"].Phase).To(Equal(v1.TaskPhasePending))
			Expect(wf.Status.TaskStatuses["c"].Phase).To(Equal(v1.TaskPhasePending))
			Expect(wf.Status.TaskStatuses["d"].Phase).To(Equal(v1.TaskPhasePending))
		})
	})

	Context("Resource constraint enforcement", func() {
		const resourceName = "constrained-wf"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			wf := &v1.AdaptiveWorkflow{}
			err := k8sClient.Get(ctx, nn, wf)
			if err != nil && errors.IsNotFound(err) {
				res := &v1.AdaptiveWorkflow{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: v1.AdaptiveWorkflowSpec{
						Tasks: []v1.TaskTemplate{
							{
								Name: "big-task-1", Image: "alpine:latest", Command: []string{"echo", "1"},
								BaseResources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
								},
							},
							{
								Name: "big-task-2", Image: "alpine:latest", Command: []string{"echo", "2"},
								BaseResources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
								},
							},
						},
						MaxResources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("300m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				}
				Expect(k8sClient.Create(ctx, res)).To(Succeed())
			}
		})

		AfterEach(func() {
			safeDeleteWorkflow(ctx, nn)
		})

		It("should not schedule both tasks if they exceed MaxResources", func() {
			r := newReconciler()

			_, err := reconcileN(ctx, r, nn, 3)
			Expect(err).NotTo(HaveOccurred())

			wf := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, nn, wf)).To(Succeed())

			runningCount := 0
			for _, ts := range wf.Status.TaskStatuses {
				if ts.Phase == v1.TaskPhaseRunning {
					runningCount++
				}
			}
			Expect(runningCount).To(Equal(1), "Only one task should run when MaxResources is tight")
		})
	})
})
