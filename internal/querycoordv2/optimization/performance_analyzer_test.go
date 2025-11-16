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

func TestPerformanceAnalyzer_AnalyzeLowRecall(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	// Set target
	target := PerformanceTarget{
		TargetLatency:    50 * time.Millisecond,
		LatencyTolerance: 1.2,
		TargetRecall:     0.95,
		RecallTolerance:  0.95,
		MemoryBudget:     10 * 1024 * 1024 * 1024,
		MemoryTolerance:  0.9,
	}
	pa.SetTarget(1, target)

	// Create metrics with low recall
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   45 * time.Millisecond,
		MeanRecall:   0.85, // Below target (0.95 * 0.95 = 0.9025)
		MemoryUsage:  5 * 1024 * 1024 * 1024,
		SampleCount:  100,
	}

	// Analyze
	analysis := pa.Analyze(metrics)
	assert.NotNil(t, analysis)
	assert.Equal(t, IssueLowRecall, analysis.Issue)
	assert.True(t, analysis.NeedsOptimization)
	assert.Greater(t, analysis.Severity, 0.0)
}

func TestPerformanceAnalyzer_AnalyzeHighLatency(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	// Set target
	target := DefaultPerformanceTarget()
	pa.SetTarget(1, target)

	// Create metrics with high latency
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   80 * time.Millisecond, // Above target (50ms * 1.2 = 60ms)
		MeanRecall:   0.95,
		MemoryUsage:  5 * 1024 * 1024 * 1024,
		SampleCount:  100,
	}

	// Analyze
	analysis := pa.Analyze(metrics)
	assert.NotNil(t, analysis)
	assert.Equal(t, IssueHighLatency, analysis.Issue)
	assert.True(t, analysis.NeedsOptimization)
	assert.Greater(t, analysis.Severity, 0.0)
}

func TestPerformanceAnalyzer_AnalyzeHighMemory(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	// Set target
	target := DefaultPerformanceTarget()
	pa.SetTarget(1, target)

	// Create metrics with high memory
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   45 * time.Millisecond,
		MeanRecall:   0.95,
		MemoryUsage:  9500 * 1024 * 1024, // Above 90% of 10GB budget
		SampleCount:  100,
	}

	// Analyze
	analysis := pa.Analyze(metrics)
	assert.NotNil(t, analysis)
	assert.Equal(t, IssueHighMemory, analysis.Issue)
	assert.True(t, analysis.NeedsOptimization)
}

func TestPerformanceAnalyzer_AnalyzeOverProvisioned(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	// Set target
	target := DefaultPerformanceTarget()
	pa.SetTarget(1, target)

	// Create metrics that are over-provisioned
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   30 * time.Millisecond, // Well below target (50ms * 0.8 = 40ms)
		MeanRecall:   0.99,                  // Well above target (0.95 * 1.05 = 0.9975)
		MemoryUsage:  5 * 1024 * 1024 * 1024,
		SampleCount:  100,
	}

	// Analyze
	analysis := pa.Analyze(metrics)
	assert.NotNil(t, analysis)
	assert.Equal(t, IssueOverProvisioned, analysis.Issue)
	assert.True(t, analysis.NeedsOptimization)
}

func TestPerformanceAnalyzer_AnalyzeNoIssue(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	// Set target
	target := DefaultPerformanceTarget()
	pa.SetTarget(1, target)

	// Create metrics that are within targets
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   50 * time.Millisecond, // At target
		MeanRecall:   0.95,                   // At target
		MemoryUsage:  5 * 1024 * 1024 * 1024, // Well below budget
		SampleCount:  100,
	}

	// Analyze
	analysis := pa.Analyze(metrics)
	assert.NotNil(t, analysis)
	assert.Equal(t, IssueNone, analysis.Issue)
	assert.False(t, analysis.NeedsOptimization)
	assert.Equal(t, 0.0, analysis.Severity)
}

func TestPerformanceAnalyzer_InsufficientSamples(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	// Create metrics with insufficient samples
	metrics := &CollectionMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		P95Latency:   100 * time.Millisecond,
		MeanRecall:   0.5,
		SampleCount:  5, // Too few samples
	}

	// Analyze
	analysis := pa.Analyze(metrics)
	assert.NotNil(t, analysis)
	assert.False(t, analysis.NeedsOptimization) // Should not optimize with few samples
}

func TestPerformanceAnalyzer_AnalyzeTrend(t *testing.T) {
	pa := NewPerformanceAnalyzer()

	previous := &CollectionMetrics{
		P95Latency: 50 * time.Millisecond,
		MeanRecall: 0.95,
	}

	// Test degrading performance
	degrading := &CollectionMetrics{
		P95Latency: 60 * time.Millisecond, // +20%
		MeanRecall: 0.90,                  // -5.3%
	}
	trend := pa.AnalyzeTrend(degrading, previous)
	assert.Equal(t, "degrading", trend)

	// Test improving performance
	improving := &CollectionMetrics{
		P95Latency: 40 * time.Millisecond, // -20%
		MeanRecall: 0.99,                  // +4.2%
	}
	trend = pa.AnalyzeTrend(improving, previous)
	assert.Equal(t, "improving", trend)

	// Test stable performance
	stable := &CollectionMetrics{
		P95Latency: 52 * time.Millisecond, // +4%
		MeanRecall: 0.94,                  // -1%
	}
	trend = pa.AnalyzeTrend(stable, previous)
	assert.Equal(t, "stable", trend)
}

func TestDefaultPerformanceTarget(t *testing.T) {
	target := DefaultPerformanceTarget()

	assert.Equal(t, 50*time.Millisecond, target.TargetLatency)
	assert.Equal(t, 1.2, target.LatencyTolerance)
	assert.Equal(t, 0.95, target.TargetRecall)
	assert.Equal(t, 0.95, target.RecallTolerance)
	assert.Equal(t, int64(10*1024*1024*1024), target.MemoryBudget)
	assert.Equal(t, 0.9, target.MemoryTolerance)
}
