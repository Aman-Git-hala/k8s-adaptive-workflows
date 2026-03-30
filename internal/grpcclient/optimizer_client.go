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
	pb "github.com/aman-githala/k8s-adaptive-workflows/proto/gen/go/optimizer"
)

// OptimizerClient implements the optimizer.Optimizer interface via gRPC.
type OptimizerClient struct {
	client pb.OptimizerServiceClient
	conn   *grpc.ClientConn
}

// NewOptimizerClient creates a new gRPC-backed optimizer client.
func NewOptimizerClient(addr string) (*OptimizerClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to optimizer at %s: %w", addr, err)
	}

	return &OptimizerClient{
		client: pb.NewOptimizerServiceClient(conn),
		conn:   conn,
	}, nil
}

// Plan implements optimizer.Optimizer.Plan via gRPC.
func (c *OptimizerClient) Plan(
	spec v1.AdaptiveWorkflowSpec,
	taskStatuses map[string]v1.TaskStatus,
	predictions optimizer.Predictions,
) (optimizer.SchedulePlan, error) {
	req := &pb.PlanRequest{
		WorkflowName:     "current",
		OptimizationGoal: spec.OptimizationGoal,
		Tasks:            make([]*pb.DAGTask, 0, len(spec.Tasks)),
		TaskStatuses:     make(map[string]*pb.TaskStatus, len(taskStatuses)),
		Predictions:      make(map[string]*pb.TaskPrediction, len(predictions)),
	}

	// Convert tasks.
	for _, task := range spec.Tasks {
		dt := &pb.DAGTask{
			Name:         task.Name,
			Image:        task.Image,
			Command:      task.Command,
			Dependencies: task.Dependencies,
		}
		if len(task.BaseResources.Requests) > 0 {
			dt.BaseResources = &pb.ResourceHint{}
			if cpu, ok := task.BaseResources.Requests[corev1.ResourceCPU]; ok {
				dt.BaseResources.CpuMillicores = cpu.MilliValue()
			}
			if mem, ok := task.BaseResources.Requests[corev1.ResourceMemory]; ok {
				dt.BaseResources.MemoryMib = mem.Value() / (1024 * 1024)
			}
		}
		req.Tasks = append(req.Tasks, dt)
	}

	// Convert task statuses.
	for name, ts := range taskStatuses {
		s := &pb.TaskStatus{
			Phase:   string(ts.Phase),
			PodName: ts.PodName,
		}
		if cpu, ok := ts.AllocatedResources.Requests[corev1.ResourceCPU]; ok {
			s.AllocatedCpuMillicores = cpu.MilliValue()
		}
		if mem, ok := ts.AllocatedResources.Requests[corev1.ResourceMemory]; ok {
			s.AllocatedMemoryMib = mem.Value() / (1024 * 1024)
		}
		req.TaskStatuses[name] = s
	}

	// Convert predictions.
	for name, p := range predictions {
		pred := &pb.TaskPrediction{
			EstimatedDurationS: p.EstimatedDuration,
		}
		if cpu, ok := p.EstimatedResources.Requests[corev1.ResourceCPU]; ok {
			pred.CpuMillicores = cpu.MilliValue()
		}
		if mem, ok := p.EstimatedResources.Requests[corev1.ResourceMemory]; ok {
			pred.MemoryMib = mem.Value() / (1024 * 1024)
		}
		req.Predictions[name] = pred
	}

	// Convert MaxResources.
	if len(spec.MaxResources) > 0 {
		req.MaxResources = &pb.ResourceLimit{}
		if cpu, ok := spec.MaxResources[corev1.ResourceCPU]; ok {
			req.MaxResources.CpuMillicores = cpu.MilliValue()
		}
		if mem, ok := spec.MaxResources[corev1.ResourceMemory]; ok {
			req.MaxResources.MemoryMib = mem.Value() / (1024 * 1024)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.client.Plan(ctx, req)
	if err != nil {
		return optimizer.SchedulePlan{}, fmt.Errorf("gRPC Plan failed: %w", err)
	}

	// Convert response → Go types.
	plan := optimizer.SchedulePlan{}
	for _, ta := range resp.TasksToStart {
		alloc := optimizer.TaskAllocation{
			TaskName: ta.TaskName,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", ta.CpuRequestMillicores)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", ta.MemoryRequestMib)),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", ta.CpuLimitMillicores)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", ta.MemoryLimitMib)),
				},
			},
		}
		plan.TasksToStart = append(plan.TasksToStart, alloc)
	}

	return plan, nil
}

// Close closes the gRPC connection.
func (c *OptimizerClient) Close() error {
	return c.conn.Close()
}
