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

// Package inference defines the interface and implementations for the
// Inference Engine. The Inference Engine predicts resource requirements
// and execution duration for workflow tasks, enabling the Optimizer
// to make data-driven scheduling decisions.
package inference

import (
	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
	"github.com/aman-githala/k8s-adaptive-workflows/internal/optimizer"
)

// Engine predicts resource needs and duration for tasks in a workflow.
// Different implementations can range from simple heuristics (trusting
// user-provided BaseResources) to ML-based models using historical data.
type Engine interface {
	// Predict takes the full workflow spec and returns per-task predictions.
	// The returned map is keyed by task name.
	Predict(spec v1.AdaptiveWorkflowSpec) (optimizer.Predictions, error)
}
