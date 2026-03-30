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

// Package main implements the Optimizer gRPC microservice.
// This wraps the existing GreedyOptimizer logic as a standalone
// gRPC service that the controller can call over the network.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
	pb "github.com/aman-githala/k8s-adaptive-workflows/proto/gen/go/optimizer"
)

type optimizerServer struct {
	pb.UnimplementedOptimizerServiceServer
	opt optimizer.Optimizer
}

// Plan implements the OptimizerService.Plan RPC.
func (s *optimizerServer) Plan(ctx context.Context, req *pb.PlanRequest) (*pb.PlanResponse, error) {
	log.Printf("Plan called for workflow %q, goal=%s, %d tasks",
		req.WorkflowName, req.OptimizationGoal, len(req.Tasks))

	// Convert proto types → Go types.
	spec := protoToSpec(req)
	taskStatuses := protoToTaskStatuses(req.TaskStatuses)
	predictions := protoToPredictions(req.Predictions)

	// Call the existing Go optimizer.
	plan, err := s.opt.Plan(spec, taskStatuses, predictions)
	if err != nil {
		return nil, fmt.Errorf("optimizer plan failed: %w", err)
	}

	// Convert Go types → proto response.
	resp := &pb.PlanResponse{}
	for _, alloc := range plan.TasksToStart {
		ta := &pb.TaskAllocation{
			TaskName: alloc.TaskName,
		}
		if cpu, ok := alloc.Resources.Requests[corev1.ResourceCPU]; ok {
			ta.CpuRequestMillicores = cpu.MilliValue()
		}
		if mem, ok := alloc.Resources.Requests[corev1.ResourceMemory]; ok {
			ta.MemoryRequestMib = mem.Value() / (1024 * 1024)
		}
		if cpu, ok := alloc.Resources.Limits[corev1.ResourceCPU]; ok {
			ta.CpuLimitMillicores = cpu.MilliValue()
		}
		if mem, ok := alloc.Resources.Limits[corev1.ResourceMemory]; ok {
			ta.MemoryLimitMib = mem.Value() / (1024 * 1024)
		}
		resp.TasksToStart = append(resp.TasksToStart, ta)
	}

	log.Printf("Plan result: %d tasks to start", len(resp.TasksToStart))
	return resp, nil
}

// ─────────────────────────────────────────────────────────────
// Proto → Go conversion helpers
// ─────────────────────────────────────────────────────────────

func protoToSpec(req *pb.PlanRequest) v1.AdaptiveWorkflowSpec {
	spec := v1.AdaptiveWorkflowSpec{
		OptimizationGoal: req.OptimizationGoal,
	}

	for _, t := range req.Tasks {
		task := v1.TaskTemplate{
			Name:         t.Name,
			Image:        t.Image,
			Command:      t.Command,
			Dependencies: t.Dependencies,
		}
		if t.BaseResources != nil {
			task.BaseResources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", t.BaseResources.CpuMillicores)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", t.BaseResources.MemoryMib)),
				},
			}
		}
		spec.Tasks = append(spec.Tasks, task)
	}

	if req.MaxResources != nil {
		spec.MaxResources = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", req.MaxResources.CpuMillicores)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", req.MaxResources.MemoryMib)),
		}
	}

	return spec
}

func protoToTaskStatuses(statuses map[string]*pb.TaskStatus) map[string]v1.TaskStatus {
	result := make(map[string]v1.TaskStatus, len(statuses))
	for name, s := range statuses {
		ts := v1.TaskStatus{
			Phase:   v1.TaskPhase(s.Phase),
			PodName: s.PodName,
		}
		if s.AllocatedCpuMillicores > 0 || s.AllocatedMemoryMib > 0 {
			ts.AllocatedResources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", s.AllocatedCpuMillicores)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", s.AllocatedMemoryMib)),
				},
			}
		}
		result[name] = ts
	}
	return result
}

func protoToPredictions(preds map[string]*pb.TaskPrediction) optimizer.Predictions {
	result := make(optimizer.Predictions, len(preds))
	for name, p := range preds {
		result[name] = optimizer.TaskPrediction{
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
	return result
}

func main() {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50052"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterOptimizerServiceServer(grpcServer, &optimizerServer{
		opt: optimizer.NewGreedyOptimizer(),
	})

	// Enable gRPC reflection for debugging with grpcurl.
	reflection.Register(grpcServer)

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down optimizer gRPC server...")
		grpcServer.GracefulStop()
	}()

	log.Printf("Optimizer gRPC server listening on :%s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
