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

// Package main implements the `kwf` CLI tool for interacting with
// AdaptiveWorkflow resources in a Kubernetes cluster.
//
// Commands:
//
//	kwf submit <file.yaml>  — Apply an AdaptiveWorkflow to the cluster
//	kwf status <name>       — Show workflow status with task-level detail
//	kwf list                — List all AdaptiveWorkflow resources
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
)

var (
	kubeconfig string
	namespace  string
)

func main() {
	// Register our CRD scheme.
	_ = v1.AddToScheme(scheme.Scheme)

	rootCmd := &cobra.Command{
		Use:   "kwf",
		Short: "kwf — CLI for K8s Adaptive Workflows",
		Long: `kwf is a command-line tool for managing AdaptiveWorkflow resources
in a Kubernetes cluster. It provides commands to submit, monitor,
and list adaptive workflow DAGs.`,
	}

	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")

	rootCmd.AddCommand(submitCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(listCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// getClient creates a controller-runtime client from kubeconfig.
func getClient() (client.Client, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	c, err := client.New(config, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return c, nil
}

// ═══════════════════════════════════════════════════════════
// kwf submit
// ═══════════════════════════════════════════════════════════

func submitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit <file.yaml>",
		Short: "Submit an AdaptiveWorkflow to the cluster",
		Long:  "Read an AdaptiveWorkflow YAML manifest and create/update it in the cluster.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			data, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", filePath, err)
			}

			// Decode YAML into an unstructured object first to get metadata.
			obj := &unstructured.Unstructured{}
			dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
			_, _, err = dec.Decode(data, nil, obj)
			if err != nil {
				return fmt.Errorf("failed to decode YAML: %w", err)
			}

			// Now decode into our typed object.
			wf := &v1.AdaptiveWorkflow{}
			_, _, err = dec.Decode(data, nil, wf)
			if err != nil {
				return fmt.Errorf("failed to decode as AdaptiveWorkflow: %w", err)
			}

			if wf.Namespace == "" {
				wf.Namespace = namespace
			}

			c, err := getClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Try to create; if it exists, update.
			existing := &v1.AdaptiveWorkflow{}
			err = c.Get(ctx, types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}, existing)
			if err != nil {
				// Create new.
				if err := c.Create(ctx, wf); err != nil {
					return fmt.Errorf("failed to create workflow: %w", err)
				}
				fmt.Printf("✓ Workflow %q submitted in namespace %q\n", wf.Name, wf.Namespace)
				fmt.Printf("  Tasks: %d\n", len(wf.Spec.Tasks))
				fmt.Printf("  Goal:  %s\n", wf.Spec.OptimizationGoal)
			} else {
				// Update existing.
				existing.Spec = wf.Spec
				if err := c.Update(ctx, existing); err != nil {
					return fmt.Errorf("failed to update workflow: %w", err)
				}
				fmt.Printf("✓ Workflow %q updated in namespace %q\n", wf.Name, wf.Namespace)
			}

			return nil
		},
	}
}

// ═══════════════════════════════════════════════════════════
// kwf status
// ═══════════════════════════════════════════════════════════

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <name>",
		Short: "Show status of an AdaptiveWorkflow",
		Long:  "Display detailed status including per-task phases, pods, and resource usage.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			c, err := getClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			wf := &v1.AdaptiveWorkflow{}
			if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, wf); err != nil {
				return fmt.Errorf("workflow %q not found: %w", name, err)
			}

			// Print header.
			fmt.Printf("Workflow: %s\n", wf.Name)
			fmt.Printf("Phase:    %s\n", wf.Status.Phase)
			fmt.Printf("Tasks:    %d total\n", len(wf.Spec.Tasks))

			if len(wf.Status.CurrentResourceUsage) > 0 {
				fmt.Printf("Resource Usage:\n")
				for r, q := range wf.Status.CurrentResourceUsage {
					fmt.Printf("  %s: %s\n", r, q.String())
				}
			}

			// Print task table.
			fmt.Println()
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TASK\tPHASE\tPOD\tCPU REQ\tMEM REQ")
			_, _ = fmt.Fprintln(w, "────\t─────\t───\t───────\t───────")

			for _, task := range wf.Spec.Tasks {
				ts, ok := wf.Status.TaskStatuses[task.Name]
				if !ok {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", task.Name, "Unknown", "-", "-", "-")
					continue
				}
				cpu := "-"
				mem := "-"
				if q, ok := ts.AllocatedResources.Requests["cpu"]; ok {
					cpu = q.String()
				}
				if q, ok := ts.AllocatedResources.Requests["memory"]; ok {
					mem = q.String()
				}
				podName := ts.PodName
				if podName == "" {
					podName = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					task.Name, ts.Phase, podName, cpu, mem)
			}
			_ = w.Flush()

			// Print conditions.
			if len(wf.Status.Conditions) > 0 {
				fmt.Println()
				fmt.Println("Conditions:")
				for _, cond := range wf.Status.Conditions {
					fmt.Printf("  %s: %s (%s) — %s\n",
						cond.Type, cond.Status, cond.Reason, cond.Message)
				}
			}

			return nil
		},
	}
}

// ═══════════════════════════════════════════════════════════
// kwf list
// ═══════════════════════════════════════════════════════════

func listCmd() *cobra.Command {
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all AdaptiveWorkflow resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := getClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			wfList := &v1.AdaptiveWorkflowList{}
			opts := []client.ListOption{}
			if !allNamespaces {
				opts = append(opts, client.InNamespace(namespace))
			}

			if err := c.List(ctx, wfList, opts...); err != nil {
				return fmt.Errorf("failed to list workflows: %w", err)
			}

			if len(wfList.Items) == 0 {
				fmt.Println("No workflows found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			if allNamespaces {
				_, _ = fmt.Fprintln(w, "NAMESPACE\tNAME\tPHASE\tTASKS\tAGE")
			} else {
				_, _ = fmt.Fprintln(w, "NAME\tPHASE\tTASKS\tAGE")
			}

			for _, wf := range wfList.Items {
				age := time.Since(wf.CreationTimestamp.Time).Round(time.Second)
				running := 0
				for _, ts := range wf.Status.TaskStatuses {
					if ts.Phase == v1.TaskPhaseRunning {
						running++
					}
				}
				taskInfo := fmt.Sprintf("%d (%d running)", len(wf.Spec.Tasks), running)

				if allNamespaces {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						wf.Namespace, wf.Name, wf.Status.Phase, taskInfo, age)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						wf.Name, wf.Status.Phase, taskInfo, age)
				}
			}
			_ = w.Flush()

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List across all namespaces")

	return cmd
}
