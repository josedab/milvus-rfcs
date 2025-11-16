// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package optimization

import (
	"fmt"

	"github.com/milvus-io/milvus/pkg/log"
	"go.uber.org/zap"
)

// OptimizationAction represents an action to optimize parameters
type OptimizationAction string

const (
	ActionIncreaseEf    OptimizationAction = "increase_ef"
	ActionDecreaseEf    OptimizationAction = "decrease_ef"
	ActionIncreaseNProbe OptimizationAction = "increase_nprobe"
	ActionDecreaseNProbe OptimizationAction = "decrease_nprobe"
	ActionRebuildSmallerM OptimizationAction = "rebuild_smaller_m"
	ActionRebuildLargerM  OptimizationAction = "rebuild_larger_m"
	ActionNoAction       OptimizationAction = "no_action"
)

// OptimizationSuggestion represents a suggested parameter change
type OptimizationSuggestion struct {
	CollectionID            int64
	Action                  OptimizationAction
	IndexType               string

	// Current parameters
	CurrentEf               int
	CurrentNProbe           int
	CurrentM                int

	// Suggested parameters
	SuggestedEf             int
	SuggestedNProbe         int
	SuggestedM              int

	// Expected impact
	ExpectedLatencyChange   float64 // -0.2 = 20% improvement (faster)
	ExpectedRecallChange    float64 // +0.03 = 3% improvement
	ExpectedMemoryChange    float64 // -0.25 = 25% reduction

	// Metadata
	RequiresReindex         bool
	Confidence              float64 // 0.0-1.0
	Reason                  string
}

// DecisionEngine makes optimization decisions based on analysis
type DecisionEngine struct {
	minSamples int
}

// NewDecisionEngine creates a new decision engine
func NewDecisionEngine(minSamples int) *DecisionEngine {
	return &DecisionEngine{
		minSamples: minSamples,
	}
}

// GenerateSuggestion generates an optimization suggestion based on analysis
func (de *DecisionEngine) GenerateSuggestion(analysis *PerformanceAnalysis) *OptimizationSuggestion {
	if analysis == nil || !analysis.NeedsOptimization {
		return nil
	}

	metrics := analysis.CurrentMetrics
	if metrics.SampleCount < int64(de.minSamples) {
		log.Debug("insufficient samples for decision",
			zap.Int64("collectionID", metrics.CollectionID),
			zap.Int64("samples", metrics.SampleCount),
			zap.Int("minRequired", de.minSamples))
		return nil
	}

	var suggestion *OptimizationSuggestion

	switch analysis.Issue {
	case IssueLowRecall:
		suggestion = de.handleLowRecall(metrics, analysis)
	case IssueHighLatency:
		suggestion = de.handleHighLatency(metrics, analysis)
	case IssueHighMemory:
		suggestion = de.handleHighMemory(metrics, analysis)
	case IssueOverProvisioned:
		suggestion = de.handleOverProvisioned(metrics, analysis)
	}

	if suggestion != nil {
		log.Info("optimization suggestion generated",
			zap.Int64("collectionID", suggestion.CollectionID),
			zap.String("action", string(suggestion.Action)),
			zap.String("reason", suggestion.Reason),
			zap.Float64("confidence", suggestion.Confidence))
	}

	return suggestion
}

// handleLowRecall generates suggestion for low recall
func (de *DecisionEngine) handleLowRecall(metrics *CollectionMetrics, analysis *PerformanceAnalysis) *OptimizationSuggestion {
	suggestion := &OptimizationSuggestion{
		CollectionID: metrics.CollectionID,
		IndexType:    metrics.IndexType,
		Confidence:   0.8,
	}

	switch metrics.IndexType {
	case "HNSW":
		currentEf := de.getIntParam(metrics.SearchParams, "ef", 64)
		suggestedEf := int(float64(currentEf) * 1.5) // Increase by 50%

		suggestion.Action = ActionIncreaseEf
		suggestion.CurrentEf = currentEf
		suggestion.SuggestedEf = suggestedEf
		suggestion.ExpectedRecallChange = 0.05  // +5% recall expected
		suggestion.ExpectedLatencyChange = 0.30 // +30% latency (slower)
		suggestion.RequiresReindex = false
		suggestion.Reason = fmt.Sprintf("Recall %.2f%% below target, increasing ef from %d to %d",
			analysis.Severity*100, currentEf, suggestedEf)

	case "IVF_FLAT", "IVF_PQ", "IVF_SQ8":
		currentNProbe := de.getIntParam(metrics.SearchParams, "nprobe", 32)
		suggestedNProbe := int(float64(currentNProbe) * 1.5) // Increase by 50%

		suggestion.Action = ActionIncreaseNProbe
		suggestion.CurrentNProbe = currentNProbe
		suggestion.SuggestedNProbe = suggestedNProbe
		suggestion.ExpectedRecallChange = 0.05  // +5% recall expected
		suggestion.ExpectedLatencyChange = 0.25 // +25% latency (slower)
		suggestion.RequiresReindex = false
		suggestion.Reason = fmt.Sprintf("Recall %.2f%% below target, increasing nprobe from %d to %d",
			analysis.Severity*100, currentNProbe, suggestedNProbe)
	}

	return suggestion
}

// handleHighLatency generates suggestion for high latency
func (de *DecisionEngine) handleHighLatency(metrics *CollectionMetrics, analysis *PerformanceAnalysis) *OptimizationSuggestion {
	suggestion := &OptimizationSuggestion{
		CollectionID: metrics.CollectionID,
		IndexType:    metrics.IndexType,
		Confidence:   0.85,
	}

	switch metrics.IndexType {
	case "HNSW":
		currentEf := de.getIntParam(metrics.SearchParams, "ef", 64)
		suggestedEf := max(32, int(float64(currentEf)*0.8)) // Decrease by 20%

		suggestion.Action = ActionDecreaseEf
		suggestion.CurrentEf = currentEf
		suggestion.SuggestedEf = suggestedEf
		suggestion.ExpectedLatencyChange = -0.20 // -20% latency (faster)
		suggestion.ExpectedRecallChange = -0.02  // -2% recall
		suggestion.RequiresReindex = false
		suggestion.Reason = fmt.Sprintf("Latency %.2f%% above target, reducing ef from %d to %d",
			analysis.Severity*100, currentEf, suggestedEf)

	case "IVF_FLAT", "IVF_PQ", "IVF_SQ8":
		currentNProbe := de.getIntParam(metrics.SearchParams, "nprobe", 32)
		suggestedNProbe := max(16, int(float64(currentNProbe)*0.75)) // Decrease by 25%

		suggestion.Action = ActionDecreaseNProbe
		suggestion.CurrentNProbe = currentNProbe
		suggestion.SuggestedNProbe = suggestedNProbe
		suggestion.ExpectedLatencyChange = -0.25 // -25% latency (faster)
		suggestion.ExpectedRecallChange = -0.03  // -3% recall
		suggestion.RequiresReindex = false
		suggestion.Reason = fmt.Sprintf("Latency %.2f%% above target, reducing nprobe from %d to %d",
			analysis.Severity*100, currentNProbe, suggestedNProbe)
	}

	return suggestion
}

// handleHighMemory generates suggestion for high memory usage
func (de *DecisionEngine) handleHighMemory(metrics *CollectionMetrics, analysis *PerformanceAnalysis) *OptimizationSuggestion {
	suggestion := &OptimizationSuggestion{
		CollectionID: metrics.CollectionID,
		IndexType:    metrics.IndexType,
		Confidence:   0.7, // Lower confidence (requires rebuild)
	}

	switch metrics.IndexType {
	case "HNSW":
		currentM := de.getIntParam(metrics.IndexParams, "M", 16)
		suggestedM := max(8, int(float64(currentM)*0.75)) // Decrease by 25%

		suggestion.Action = ActionRebuildSmallerM
		suggestion.CurrentM = currentM
		suggestion.SuggestedM = suggestedM
		suggestion.ExpectedMemoryChange = -0.25 // -25% memory
		suggestion.ExpectedRecallChange = -0.02 // -2% recall
		suggestion.RequiresReindex = true
		suggestion.Reason = fmt.Sprintf("Memory usage %.2f%% above budget, rebuilding with M from %d to %d",
			analysis.Severity*100, currentM, suggestedM)
	}

	return suggestion
}

// handleOverProvisioned generates suggestion for over-provisioned systems
func (de *DecisionEngine) handleOverProvisioned(metrics *CollectionMetrics, analysis *PerformanceAnalysis) *OptimizationSuggestion {
	suggestion := &OptimizationSuggestion{
		CollectionID: metrics.CollectionID,
		IndexType:    metrics.IndexType,
		Confidence:   0.75,
	}

	switch metrics.IndexType {
	case "HNSW":
		currentEf := de.getIntParam(metrics.SearchParams, "ef", 64)
		suggestedEf := max(32, int(float64(currentEf)*0.75)) // Decrease by 25%

		suggestion.Action = ActionDecreaseEf
		suggestion.CurrentEf = currentEf
		suggestion.SuggestedEf = suggestedEf
		suggestion.ExpectedLatencyChange = -0.15 // -15% latency (faster)
		suggestion.ExpectedRecallChange = -0.02  // -2% recall (still acceptable)
		suggestion.RequiresReindex = false
		suggestion.Reason = fmt.Sprintf("Over-provisioned: can reduce ef from %d to %d to save resources",
			currentEf, suggestedEf)

	case "IVF_FLAT", "IVF_PQ", "IVF_SQ8":
		currentNProbe := de.getIntParam(metrics.SearchParams, "nprobe", 32)
		suggestedNProbe := max(16, int(float64(currentNProbe)*0.75)) // Decrease by 25%

		suggestion.Action = ActionDecreaseNProbe
		suggestion.CurrentNProbe = currentNProbe
		suggestion.SuggestedNProbe = suggestedNProbe
		suggestion.ExpectedLatencyChange = -0.20 // -20% latency (faster)
		suggestion.ExpectedRecallChange = -0.02  // -2% recall (still acceptable)
		suggestion.RequiresReindex = false
		suggestion.Reason = fmt.Sprintf("Over-provisioned: can reduce nprobe from %d to %d to save resources",
			currentNProbe, suggestedNProbe)
	}

	return suggestion
}

// getIntParam safely extracts integer parameter with default
func (de *DecisionEngine) getIntParam(params map[string]interface{}, key string, defaultValue int) int {
	if params == nil {
		return defaultValue
	}

	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}

	return defaultValue
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
