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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDecisionEngine_GenerateSuggestionLowRecall(t *testing.T) {
	de := NewDecisionEngine(10)

	// Create analysis for low recall on HNSW
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		MeanRecall:   0.85,
		SampleCount:  100,
		SearchParams: map[string]interface{}{"ef": 64},
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueLowRecall,
		Severity:          0.1,
		NeedsOptimization: true,
		CurrentMetrics:    metrics,
	}

	// Generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.NotNil(t, suggestion)
	assert.Equal(t, ActionIncreaseEf, suggestion.Action)
	assert.Equal(t, 64, suggestion.CurrentEf)
	assert.Equal(t, 96, suggestion.SuggestedEf) // 64 * 1.5
	assert.False(t, suggestion.RequiresReindex)
	assert.Greater(t, suggestion.ExpectedRecallChange, 0.0)
}

func TestDecisionEngine_GenerateSuggestionHighLatency(t *testing.T) {
	de := NewDecisionEngine(10)

	// Create analysis for high latency on HNSW
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   80 * time.Millisecond,
		SampleCount:  100,
		SearchParams: map[string]interface{}{"ef": 64},
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueHighLatency,
		Severity:          0.5,
		NeedsOptimization: true,
		CurrentMetrics:    metrics,
	}

	// Generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.NotNil(t, suggestion)
	assert.Equal(t, ActionDecreaseEf, suggestion.Action)
	assert.Equal(t, 64, suggestion.CurrentEf)
	assert.Equal(t, 51, suggestion.SuggestedEf) // 64 * 0.8 = 51.2
	assert.False(t, suggestion.RequiresReindex)
	assert.Less(t, suggestion.ExpectedLatencyChange, 0.0) // Negative = improvement
}

func TestDecisionEngine_GenerateSuggestionIVF(t *testing.T) {
	de := NewDecisionEngine(10)

	// Create analysis for high latency on IVF_FLAT
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "IVF_FLAT",
		P95Latency:   80 * time.Millisecond,
		SampleCount:  100,
		SearchParams: map[string]interface{}{"nprobe": 32},
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueHighLatency,
		Severity:          0.5,
		NeedsOptimization: true,
		CurrentMetrics:    metrics,
	}

	// Generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.NotNil(t, suggestion)
	assert.Equal(t, ActionDecreaseNProbe, suggestion.Action)
	assert.Equal(t, 32, suggestion.CurrentNProbe)
	assert.Equal(t, 24, suggestion.SuggestedNProbe) // 32 * 0.75
	assert.False(t, suggestion.RequiresReindex)
}

func TestDecisionEngine_GenerateSuggestionHighMemory(t *testing.T) {
	de := NewDecisionEngine(10)

	// Create analysis for high memory on HNSW
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		MemoryUsage:  9 * 1024 * 1024 * 1024,
		SampleCount:  100,
		IndexParams:  map[string]interface{}{"M": 16},
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueHighMemory,
		Severity:          0.8,
		NeedsOptimization: true,
		CurrentMetrics:    metrics,
	}

	// Generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.NotNil(t, suggestion)
	assert.Equal(t, ActionRebuildSmallerM, suggestion.Action)
	assert.Equal(t, 16, suggestion.CurrentM)
	assert.Equal(t, 12, suggestion.SuggestedM) // 16 * 0.75
	assert.True(t, suggestion.RequiresReindex)
	assert.Less(t, suggestion.ExpectedMemoryChange, 0.0) // Negative = reduction
}

func TestDecisionEngine_GenerateSuggestionOverProvisioned(t *testing.T) {
	de := NewDecisionEngine(10)

	// Create analysis for over-provisioned HNSW
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   30 * time.Millisecond,
		MeanRecall:   0.99,
		SampleCount:  100,
		SearchParams: map[string]interface{}{"ef": 128},
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueOverProvisioned,
		Severity:          0.5,
		NeedsOptimization: true,
		CurrentMetrics:    metrics,
	}

	// Generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.NotNil(t, suggestion)
	assert.Equal(t, ActionDecreaseEf, suggestion.Action)
	assert.Equal(t, 128, suggestion.CurrentEf)
	assert.Equal(t, 96, suggestion.SuggestedEf) // 128 * 0.75
	assert.False(t, suggestion.RequiresReindex)
}

func TestDecisionEngine_NoSuggestionInsufficientSamples(t *testing.T) {
	de := NewDecisionEngine(100) // Require 100 samples

	// Create analysis with insufficient samples
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		SampleCount:  50, // Below minimum
		SearchParams: map[string]interface{}{"ef": 64},
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueHighLatency,
		NeedsOptimization: true,
		CurrentMetrics:    metrics,
	}

	// Should not generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.Nil(t, suggestion)
}

func TestDecisionEngine_NoSuggestionNoOptimization(t *testing.T) {
	de := NewDecisionEngine(10)

	// Create analysis with no optimization needed
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		SampleCount:  100,
	}

	analysis := &PerformanceAnalysis{
		CollectionID:      1,
		Issue:             IssueNone,
		NeedsOptimization: false,
		CurrentMetrics:    metrics,
	}

	// Should not generate suggestion
	suggestion := de.GenerateSuggestion(analysis)
	assert.Nil(t, suggestion)
}

func TestDecisionEngine_GetIntParam(t *testing.T) {
	de := NewDecisionEngine(10)

	// Test with int
	params := map[string]interface{}{
		"ef": 64,
	}
	value := de.getIntParam(params, "ef", 32)
	assert.Equal(t, 64, value)

	// Test with int64
	params = map[string]interface{}{
		"ef": int64(64),
	}
	value = de.getIntParam(params, "ef", 32)
	assert.Equal(t, 64, value)

	// Test with float64
	params = map[string]interface{}{
		"ef": float64(64.0),
	}
	value = de.getIntParam(params, "ef", 32)
	assert.Equal(t, 64, value)

	// Test with default
	params = map[string]interface{}{}
	value = de.getIntParam(params, "ef", 32)
	assert.Equal(t, 32, value)

	// Test with nil
	value = de.getIntParam(nil, "ef", 32)
	assert.Equal(t, 32, value)
}

func TestMax(t *testing.T) {
	assert.Equal(t, 10, max(10, 5))
	assert.Equal(t, 10, max(5, 10))
	assert.Equal(t, 10, max(10, 10))
}
