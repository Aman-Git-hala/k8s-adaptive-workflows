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

package inference

import (
	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
)

// BaselineEngine is the simplest Inference Engine implementation.
// It trusts the user's BaseResources hints as-is and returns them
// as predictions. If a task has no BaseResources, it returns no
// prediction for that task, letting the Optimizer fall back to defaults.
//
// This is the "v1" implementation. A future version could use historical
// execution data to produce more accurate predictions.
type BaselineEngine struct{}

// NewBaselineEngine creates a new BaselineEngine.
func NewBaselineEngine() *BaselineEngine {
	return &BaselineEngine{}
}

// Predict implements Engine.Predict by passing through BaseResources.
func (b *BaselineEngine) Predict(spec v1.AdaptiveWorkflowSpec) (optimizer.Predictions, error) {
	predictions := make(optimizer.Predictions, len(spec.Tasks))

	for _, task := range spec.Tasks {
		// Only produce a prediction if the user provided a hint.
		if len(task.BaseResources.Requests) > 0 || len(task.BaseResources.Limits) > 0 {
			predictions[task.Name] = optimizer.TaskPrediction{
				EstimatedDuration:  0, // Unknown — no historical data yet.
				EstimatedResources: task.BaseResources,
			}
		}
		// If no BaseResources, we intentionally omit the prediction.
		// The Optimizer will fall back to its own default.
	}

	return predictions, nil
}
