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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/inference"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/statedb"
)

// newReconciler creates an AdaptiveWorkflowReconciler wired to the envtest k8sClient.
func newReconciler() *AdaptiveWorkflowReconciler {
	return &AdaptiveWorkflowReconciler{
		Client:    k8sClient,
		Scheme:    scheme.Scheme,
		Inference: inference.NewBaselineEngine(),
		Optimizer: optimizer.NewGreedyOptimizer(),
		StateDB:   statedb.NewNoOpStateDB(),
		Recorder:  record.NewFakeRecorder(100),
	}
}

// reconcileN calls Reconcile up to n times, stopping early if the result
// has no requeue request. Returns the last result.
func reconcileN(r *AdaptiveWorkflowReconciler, req reconcile.Request, n int) (ctrl.Result, error) {
	var res ctrl.Result
	var err error
	for i := 0; i < n; i++ {
		res, err = r.Reconcile(ctx, req)
		if err != nil || (!res.Requeue && res.RequeueAfter == 0) {
			return res, err
		}
	}
	return res, err
}

// makeWorkflow builds an AdaptiveWorkflow for testing.
func makeWorkflow(name, namespace string, tasks []v1.TaskTemplate, maxRes corev1.ResourceList) *v1.AdaptiveWorkflow {
	return &v1.AdaptiveWorkflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.AdaptiveWorkflowSpec{
			Tasks:            tasks,
			MaxResources:     maxRes,
			OptimizationGoal: "MinimizeTime",
		},
	}
}

var _ = Describe("AdaptiveWorkflow Controller", func() {
	var (
		r         *AdaptiveWorkflowReconciler
		ns        string
		wfCounter int
	)

	BeforeEach(func() {
		r = newReconciler()
		wfCounter++
		ns = fmt.Sprintf("test-ns-%d", wfCounter)
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	})

	AfterEach(func() {
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
	})

	Context("Initialization", func() {
		It("initializes TaskStatuses and sets phase to Pending on first reconcile", func() {
			wf := makeWorkflow("wf-init", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
				{Name: "task-b", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// First reconcile initializes status and requeues.
			res, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeTrue())

			// Reload from k8s to get updated status.
			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())

			Expect(updated.Status.TaskStatuses).To(HaveLen(2))
			Expect(updated.Status.TaskStatuses["task-a"].Phase).To(Equal(v1.TaskPhasePending))
			Expect(updated.Status.TaskStatuses["task-b"].Phase).To(Equal(v1.TaskPhasePending))
			Expect(updated.Status.Phase).To(Equal(v1.WorkflowPhasePending))
		})

		It("adds the workflow finalizer on creation", func() {
			wf := makeWorkflow("wf-finalizer", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// After the first reconcile the finalizer should be present.
			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(workflowFinalizer))
		})
	})

	Context("DAG scheduling - root tasks (no dependencies)", func() {
		It("creates pods for all root tasks on the second reconcile", func() {
			wf := makeWorkflow("wf-roots", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
				{Name: "task-b", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// 1st reconcile: initialization
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// 2nd reconcile: scheduling
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Both root tasks should have pods created.
			podA := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, podA)).To(Succeed())

			podB := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-b"), Namespace: ns,
			}, podB)).To(Succeed())
		})

		It("transitions root tasks to Running phase after pod creation", func() {
			wf := makeWorkflow("wf-running", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			// init reconcile
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// scheduling reconcile
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			Expect(updated.Status.TaskStatuses["task-a"].Phase).To(Equal(v1.TaskPhaseRunning))
			Expect(updated.Status.Phase).To(Equal(v1.WorkflowPhaseRunning))
		})
	})

	Context("DAG scheduling - dependency ordering", func() {
		It("does not schedule downstream tasks until their dependencies succeed", func() {
			wf := makeWorkflow("wf-dag", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
				{Name: "task-b", Image: "busybox", Dependencies: []string{"task-a"}},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// 1st reconcile: initialization
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// 2nd reconcile: schedules task-a (no deps), not task-b
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// task-a pod should exist
			podA := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, podA)).To(Succeed())

			// task-b pod must NOT exist yet
			podB := &corev1.Pod{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-b"), Namespace: ns,
			}, podB)
			Expect(err).To(HaveOccurred()) // NotFound
		})

		It("schedules task-b after task-a is manually marked Succeeded", func() {
			wf := makeWorkflow("wf-seq", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
				{Name: "task-b", Image: "busybox", Dependencies: []string{"task-a"}},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// 1st reconcile: initialization
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// 2nd reconcile: schedules task-a
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Manually mark task-a pod as Succeeded so that syncPodsToStatus picks it up.
			podA := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, podA)).To(Succeed())
			podA.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, podA)).To(Succeed())

			// 3rd reconcile: syncPodsToStatus sees task-a Succeeded, schedules task-b
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			Expect(updated.Status.TaskStatuses["task-a"].Phase).To(Equal(v1.TaskPhaseSucceeded))

			// task-b pod must now exist
			podB := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-b"), Namespace: ns,
			}, podB)).To(Succeed())
		})
	})

	Context("Workflow completion", func() {
		It("transitions to Completed when all tasks Succeed", func() {
			wf := makeWorkflow("wf-complete", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// init
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// schedule
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Mark pod Succeeded.
			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, pod)).To(Succeed())
			pod.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Reconcile sees the completed pod and sets workflow Completed.
			res, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// Should not requeue once completed.
			Expect(res.RequeueAfter).To(BeZero())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(v1.WorkflowPhaseCompleted))
			Expect(updated.Status.TaskStatuses["task-a"].Phase).To(Equal(v1.TaskPhaseSucceeded))
		})

		It("does not requeue a completed workflow", func() {
			wf := makeWorkflow("wf-done", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			reconcileN(r, req, 2) //nolint:errcheck

			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, pod)).To(Succeed())
			pod.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Third reconcile should complete the workflow.
			reconcileN(r, req, 1) //nolint:errcheck

			// Any subsequent reconcile of a completed workflow should be a no-op.
			res, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())
			Expect(res.RequeueAfter).To(BeZero())
		})
	})

	Context("Workflow failure", func() {
		It("transitions to Failed when any task fails", func() {
			wf := makeWorkflow("wf-fail", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
				{Name: "task-b", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// init + schedule
			reconcileN(r, req, 2) //nolint:errcheck

			// Mark task-a pod as Failed.
			podA := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, podA)).To(Succeed())
			podA.Status.Phase = corev1.PodFailed
			Expect(k8sClient.Status().Update(ctx, podA)).To(Succeed())

			// Reconcile detects failure.
			res, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(v1.WorkflowPhaseFailed))
		})
	})

	Context("Resource constraints", func() {
		It("throttles task scheduling when MaxResources would be exceeded", func() {
			// MaxResources only allows 200m CPU total.
			// Each task requests 200m CPU via BaseResources.
			// So only one task can run at a time.
			maxRes := corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("200m"),
			}
			wf := makeWorkflow("wf-throttle", ns, []v1.TaskTemplate{
				{
					Name:  "task-a",
					Image: "busybox",
					BaseResources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("200m"),
						},
					},
				},
				{
					Name:  "task-b",
					Image: "busybox",
					BaseResources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("200m"),
						},
					},
				},
			}, maxRes)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			reconcileN(r, req, 2) //nolint:errcheck

			// Only one of the two pods should have been created.
			podAExists := false
			podBExists := false

			podA := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, podA); err == nil {
				podAExists = true
			}
			podB := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-b"), Namespace: ns,
			}, podB); err == nil {
				podBExists = true
			}

			// Exactly one task scheduled.
			Expect(podAExists != podBExists).To(BeTrue(), "expected exactly one task to be scheduled")
		})
	})

	Context("Status conditions", func() {
		It("sets Progressing=True on initialization", func() {
			wf := makeWorkflow("wf-cond", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())

			var progressingCond *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "Progressing" {
					progressingCond = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(progressingCond).NotTo(BeNil())
			Expect(progressingCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("sets Available=True on workflow completion", func() {
			wf := makeWorkflow("wf-avail", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			reconcileN(r, req, 2) //nolint:errcheck

			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: podName(wf.Name, "task-a"), Namespace: ns,
			}, pod)).To(Succeed())
			pod.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())

			var availCond *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "Available" {
					availCond = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(availCond).NotTo(BeNil())
			Expect(availCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("podName helper", func() {
		It("generates deterministic lowercase pod names", func() {
			Expect(podName("my-Workflow", "My-Task")).To(Equal("my-workflow-my-task"))
			Expect(podName("wf", "t1")).To(Equal("wf-t1"))
		})
	})

	Context("computeResourceUsage", func() {
		It("sums CPU and memory for Running tasks only", func() {
			cpu200m := resource.MustParse("200m")
			mem256Mi := resource.MustParse("256Mi")

			statuses := map[string]v1.TaskStatus{
				"task-a": {
					Phase: v1.TaskPhaseRunning,
					AllocatedResources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    cpu200m,
							corev1.ResourceMemory: mem256Mi,
						},
					},
				},
				"task-b": {
					Phase: v1.TaskPhaseSucceeded, // Should NOT be counted.
					AllocatedResources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
				"task-c": {
					Phase: v1.TaskPhasePending, // Should NOT be counted.
				},
			}

			usage := r.computeResourceUsage(statuses)
			Expect(usage).NotTo(BeNil())
			gotCPU := usage[corev1.ResourceCPU]
			gotMem := usage[corev1.ResourceMemory]
			Expect(gotCPU.Cmp(cpu200m)).To(Equal(0))
			Expect(gotMem.Cmp(mem256Mi)).To(Equal(0))
		})

		It("returns nil when no tasks are Running", func() {
			statuses := map[string]v1.TaskStatus{
				"task-a": {Phase: v1.TaskPhaseSucceeded},
			}
			usage := r.computeResourceUsage(statuses)
			Expect(usage).To(BeNil())
		})
	})

	Context("Non-existent workflow", func() {
		It("returns no error for a missing AdaptiveWorkflow", func() {
			req := reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      "does-not-exist",
				Namespace: ns,
			}}
			res, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())
		})
	})

	Context("Deletion / finalizer cleanup", func() {
		It("removes the finalizer when the workflow is deleted", func() {
			wf := makeWorkflow("wf-del", ns, []v1.TaskTemplate{
				{Name: "task-a", Image: "busybox"},
			}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}

			// Reconcile to add finalizer.
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Delete the workflow.
			Expect(k8sClient.Delete(ctx, wf)).To(Succeed())

			// Reconcile should process deletion and remove finalizer.
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &v1.AdaptiveWorkflow{}
			err = k8sClient.Get(ctx, req.NamespacedName, updated)
			// Either the object is gone or it has no finalizer.
			if err == nil {
				Expect(updated.Finalizers).NotTo(ContainElement(workflowFinalizer))
			}
			// If NotFound, that is also acceptable.
		})
	})

	// Ensure that the allSucceeded guard works correctly for empty-task workflows.
	Context("Empty task list", func() {
		It("does not transition to Completed on zero tasks", func() {
			wf := makeWorkflow("wf-empty", ns, []v1.TaskTemplate{}, nil)
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: wf.Name, Namespace: ns}}
			// init
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// schedule (nothing to schedule)
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &v1.AdaptiveWorkflow{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			// An empty workflow should stay Running (not spuriously Completed).
			Expect(updated.Status.Phase).NotTo(Equal(v1.WorkflowPhaseCompleted))
		})
	})
})

// Ensure timeout constant is exported for testing.
var _ = time.Second
