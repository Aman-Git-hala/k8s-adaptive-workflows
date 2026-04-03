package controller

import (
	"context"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/statedb"
)

// MockInferenceEngine implements inference.Engine for testing.
type MockInferenceEngine struct {
	PredictFunc func(spec v1.AdaptiveWorkflowSpec) (optimizer.Predictions, error)
}

func (m *MockInferenceEngine) Predict(spec v1.AdaptiveWorkflowSpec) (optimizer.Predictions, error) {
	if m.PredictFunc != nil {
		return m.PredictFunc(spec)
	}
	return optimizer.Predictions{}, nil
}

// MockOptimizer implements optimizer.Optimizer for testing.
type MockOptimizer struct {
	PlanFunc func(spec v1.AdaptiveWorkflowSpec, taskStatuses map[string]v1.TaskStatus, predictions optimizer.Predictions) (optimizer.SchedulePlan, error)
}

func (m *MockOptimizer) Plan(spec v1.AdaptiveWorkflowSpec, taskStatuses map[string]v1.TaskStatus, predictions optimizer.Predictions) (optimizer.SchedulePlan, error) {
	if m.PlanFunc != nil {
		return m.PlanFunc(spec, taskStatuses, predictions)
	}
	// By default, just return an empty plan.
	return optimizer.SchedulePlan{}, nil
}

// MockStateDB implements statedb.StateDB for testing.
type MockStateDB struct {
	RecordExecutionFunc   func(ctx context.Context, exec statedb.TaskExecution) error
	GetHistoryByImageFunc func(ctx context.Context, image string) (*statedb.HistoricalStats, error)
	CloseFunc             func() error

	RecordedExecutions []statedb.TaskExecution
}

func (m *MockStateDB) RecordExecution(ctx context.Context, exec statedb.TaskExecution) error {
	m.RecordedExecutions = append(m.RecordedExecutions, exec)
	if m.RecordExecutionFunc != nil {
		return m.RecordExecutionFunc(ctx, exec)
	}
	return nil
}

func (m *MockStateDB) GetHistoryByImage(ctx context.Context, image string) (*statedb.HistoricalStats, error) {
	if m.GetHistoryByImageFunc != nil {
		return m.GetHistoryByImageFunc(ctx, image)
	}
	return &statedb.HistoricalStats{Image: image}, nil
}

func (m *MockStateDB) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
