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

// Package grpcclient provides gRPC client wrappers that implement the
// Inference Engine and Optimizer interfaces, allowing the controller
// to call remote gRPC services instead of in-process Go implementations.
package grpcclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
	pb "github.com/aman-githala/k8s-adaptive-workflows/proto/gen/go/inference"
)

// InferenceClient implements the inference.Engine interface via gRPC.
type InferenceClient struct {
	client pb.InferenceServiceClient
	conn   *grpc.ClientConn
}

// NewInferenceClient creates a new gRPC-backed inference engine client.
func NewInferenceClient(addr string) (*InferenceClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to inference engine at %s: %w", addr, err)
	}

	return &InferenceClient{
		client: pb.NewInferenceServiceClient(conn),
		conn:   conn,
	}, nil
}

// Predict implements inference.Engine.Predict via gRPC.
func (c *InferenceClient) Predict(spec v1.AdaptiveWorkflowSpec) (optimizer.Predictions, error) {
	req := &pb.PredictRequest{
		WorkflowName: "current", // Will be set properly when called from controller
		Tasks:        make([]*pb.TaskInfo, 0, len(spec.Tasks)),
	}

	for _, task := range spec.Tasks {
		ti := &pb.TaskInfo{
			Name:         task.Name,
			Image:        task.Image,
			Command:      task.Command,
			Dependencies: task.Dependencies,
		}
		if len(task.BaseResources.Requests) > 0 {
			ti.BaseResources = &pb.ResourceHint{}
			if cpu, ok := task.BaseResources.Requests[corev1.ResourceCPU]; ok {
				ti.BaseResources.CpuMillicores = cpu.MilliValue()
			}
			if mem, ok := task.BaseResources.Requests[corev1.ResourceMemory]; ok {
				ti.BaseResources.MemoryMib = mem.Value() / (1024 * 1024)
			}
		}
		req.Tasks = append(req.Tasks, ti)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.client.Predict(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gRPC Predict failed: %w", err)
	}

	// Convert proto predictions → Go predictions.
	predictions := make(optimizer.Predictions, len(resp.Predictions))
	for name, p := range resp.Predictions {
		predictions[name] = optimizer.TaskPrediction{
			EstimatedDuration: p.EstimatedDurationS,
			EstimatedResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", p.CpuMillicores)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", p.MemoryMib)),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int64(float64(p.CpuMillicores)*1.5))),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", int64(float64(p.MemoryMib)*1.5))),
				},
			},
		}
	}

	return predictions, nil
}

// ReportMetrics sends actual execution data back to the inference engine.
func (c *InferenceClient) ReportMetrics(ctx context.Context, workflowName string, metrics []*pb.TaskMetrics) error {
	req := &pb.ReportMetricsRequest{
		WorkflowName: workflowName,
		TaskMetrics:  metrics,
	}

	resp, err := c.client.ReportMetrics(ctx, req)
	if err != nil {
		return fmt.Errorf("gRPC ReportMetrics failed: %w", err)
	}
	if !resp.Accepted {
		return fmt.Errorf("metrics were not accepted by inference engine")
	}
	return nil
}

// Close closes the gRPC connection.
func (c *InferenceClient) Close() error {
	return c.conn.Close()
}
