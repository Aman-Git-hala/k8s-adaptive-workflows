package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
)

var _ = Describe("AdaptiveWorkflow Controller", func() {
	const (
		WorkflowName      = "test-workflow"
		WorkflowNamespace = "default"
		timeout           = time.Second * 10
		interval          = time.Millisecond * 250
	)

	Context("When creating a new AdaptiveWorkflow", func() {
		It("should initialize TaskStatuses, spawn pods and handle completion", func() {
			ctx := context.Background()

			// Configure Optimizer Mock to just launch pending tasks
			mockOpt.PlanFunc = func(spec v1.AdaptiveWorkflowSpec, taskStatuses map[string]v1.TaskStatus, predictions optimizer.Predictions) (optimizer.SchedulePlan, error) {
				var plan optimizer.SchedulePlan
				for _, t := range spec.Tasks {
					if taskStatuses[t.Name].Phase == v1.TaskPhasePending {
						plan.TasksToStart = append(plan.TasksToStart, optimizer.TaskAllocation{
							TaskName: t.Name,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						})
					}
				}
				return plan, nil
			}

			// Define workflow
			wf := &v1.AdaptiveWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      WorkflowName,
					Namespace: WorkflowNamespace,
				},
				Spec: v1.AdaptiveWorkflowSpec{
					Tasks: []v1.TaskTemplate{
						{
							Name:    "task1",
							Image:   "busybox",
							Command: []string{"sleep", "1"},
						},
						{
							Name:    "task2",
							Image:   "busybox",
							Command: []string{"sleep", "1"},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, wf)).Should(Succeed())

			// 1. Check Initialization
			wfLookupKey := types.NamespacedName{Name: WorkflowName, Namespace: WorkflowNamespace}
			createdWf := &v1.AdaptiveWorkflow{}

			// Check that tasks become Running eventually
			Eventually(func() string {
				err := k8sClient.Get(ctx, wfLookupKey, createdWf)
				if err != nil {
					return ""
				}
				return string(createdWf.Status.Phase)
			}, timeout, interval).Should(Equal(string(v1.WorkflowPhaseRunning)))

			Expect(createdWf.Status.TaskStatuses).To(HaveLen(2))
			Expect(string(createdWf.Status.TaskStatuses["task1"].Phase)).To(Equal(string(v1.TaskPhaseRunning)))
			Expect(string(createdWf.Status.TaskStatuses["task2"].Phase)).To(Equal(string(v1.TaskPhaseRunning)))

			// 2. Verify pods are created
			var podList corev1.PodList
			Eventually(func() int {
				err := k8sClient.List(ctx, &podList, client.InNamespace(WorkflowNamespace), client.MatchingLabels{"adaptive-workflow": WorkflowName})
				if err != nil {
					return -1
				}
				return len(podList.Items)
			}, timeout, interval).Should(Equal(2))

			// 3. Simulate Pod success transition
			// Update the pods to Succeeded phase
			for _, pod := range podList.Items {
				pod.Status.Phase = corev1.PodSucceeded
				Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed()) // Uses Status updater
			}

			// Verify transition to Workflow Completed
			Eventually(func() string {
				_ = k8sClient.Get(ctx, wfLookupKey, createdWf)
				return string(createdWf.Status.Phase)
			}, timeout, interval).Should(Equal(string(v1.WorkflowPhaseCompleted)))
		})
	})
})
