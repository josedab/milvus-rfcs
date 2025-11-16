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
	"time"

	"github.com/milvus-io/milvus/pkg/log"
	"go.uber.org/zap"
)

// PerformanceIssue represents a detected performance issue
type PerformanceIssue string

const (
	IssueHighLatency      PerformanceIssue = "high_latency"
	IssueLowRecall        PerformanceIssue = "low_recall"
	IssueHighMemory       PerformanceIssue = "high_memory"
	IssueOverProvisioned  PerformanceIssue = "over_provisioned"
	IssueNone             PerformanceIssue = "none"
)

// PerformanceAnalysis represents the result of analyzing metrics
type PerformanceAnalysis struct {
	CollectionID    int64
	Issue           PerformanceIssue
	Severity        float64 // 0.0 = no issue, 1.0 = critical
	Description     string
	CurrentMetrics  *CollectionMetrics
	NeedsOptimization bool
}

// PerformanceTarget defines SLA targets for a collection
type PerformanceTarget struct {
	// Latency target (P95)
	TargetLatency    time.Duration
	LatencyTolerance float64 // e.g., 1.2 = 20% over target is acceptable

	// Recall target
	TargetRecall     float64
	RecallTolerance  float64 // e.g., 0.95 = must be at least 95% of target

	// Memory budget
	MemoryBudget     int64 // in bytes
	MemoryTolerance  float64 // e.g., 0.9 = start optimizing at 90% of budget
}

// DefaultPerformanceTarget returns default targets
func DefaultPerformanceTarget() PerformanceTarget {
	return PerformanceTarget{
		TargetLatency:    50 * time.Millisecond,
		LatencyTolerance: 1.2, // 20% over is acceptable
		TargetRecall:     0.95,
		RecallTolerance:  0.95, // Must be at least 95% of target (0.9025)
		MemoryBudget:     10 * 1024 * 1024 * 1024, // 10GB
		MemoryTolerance:  0.9, // Start optimizing at 90%
	}
}

// PerformanceAnalyzer analyzes metrics to detect issues
type PerformanceAnalyzer struct {
	targets map[int64]PerformanceTarget // collectionID -> targets
}

// NewPerformanceAnalyzer creates a new performance analyzer
func NewPerformanceAnalyzer() *PerformanceAnalyzer {
	return &PerformanceAnalyzer{
		targets: make(map[int64]PerformanceTarget),
	}
}

// SetTarget sets performance targets for a collection
func (pa *PerformanceAnalyzer) SetTarget(collectionID int64, target PerformanceTarget) {
	pa.targets[collectionID] = target
}

// GetTarget gets performance targets for a collection (or default)
func (pa *PerformanceAnalyzer) GetTarget(collectionID int64) PerformanceTarget {
	if target, ok := pa.targets[collectionID]; ok {
		return target
	}
	return DefaultPerformanceTarget()
}

// Analyze analyzes collection metrics and detects issues
func (pa *PerformanceAnalyzer) Analyze(metrics *CollectionMetrics) *PerformanceAnalysis {
	if metrics == nil {
		return nil
	}

	target := pa.GetTarget(metrics.CollectionID)
	analysis := &PerformanceAnalysis{
		CollectionID:   metrics.CollectionID,
		CurrentMetrics: metrics,
		Issue:          IssueNone,
		Severity:       0.0,
	}

	// Check for insufficient samples
	if metrics.SampleCount < 10 {
		log.Debug("insufficient samples for analysis",
			zap.Int64("collectionID", metrics.CollectionID),
			zap.Int64("sampleCount", metrics.SampleCount))
		return analysis
	}

	// Priority 1: Check recall (most critical)
	if metrics.MeanRecall < target.TargetRecall*target.RecallTolerance {
		severity := (target.TargetRecall - metrics.MeanRecall) / target.TargetRecall
		if severity > analysis.Severity {
			analysis.Issue = IssueLowRecall
			analysis.Severity = severity
			analysis.NeedsOptimization = true
			analysis.Description = "Recall below target, accuracy needs improvement"
		}
	}

	// Priority 2: Check latency
	if metrics.P95Latency > time.Duration(float64(target.TargetLatency)*target.LatencyTolerance) {
		severity := float64(metrics.P95Latency-target.TargetLatency) / float64(target.TargetLatency)
		if severity > analysis.Severity {
			analysis.Issue = IssueHighLatency
			analysis.Severity = severity
			analysis.NeedsOptimization = true
			analysis.Description = "P95 latency exceeds target, speed needs improvement"
		}
	}

	// Priority 3: Check memory
	if metrics.MemoryUsage > int64(float64(target.MemoryBudget)*target.MemoryTolerance) {
		severity := float64(metrics.MemoryUsage-target.MemoryBudget) / float64(target.MemoryBudget)
		if severity > analysis.Severity {
			analysis.Issue = IssueHighMemory
			analysis.Severity = severity
			analysis.NeedsOptimization = true
			analysis.Description = "Memory usage approaching budget limit"
		}
	}

	// Priority 4: Check over-provisioning (opportunity to save costs)
	// Only if no critical issues detected
	if analysis.Severity < 0.1 {
		isOverProvisioned := metrics.MeanRecall > target.TargetRecall*1.05 && // Recall significantly higher than needed
			metrics.P95Latency < time.Duration(float64(target.TargetLatency)*0.8) // Latency well below target

		if isOverProvisioned {
			analysis.Issue = IssueOverProvisioned
			analysis.Severity = 0.5 // Medium priority
			analysis.NeedsOptimization = true
			analysis.Description = "System over-provisioned, can reduce resources to save costs"
		}
	}

	if analysis.NeedsOptimization {
		log.Info("performance issue detected",
			zap.Int64("collectionID", metrics.CollectionID),
			zap.String("issue", string(analysis.Issue)),
			zap.Float64("severity", analysis.Severity),
			zap.String("description", analysis.Description))
	}

	return analysis
}

// AnalyzeTrend analyzes performance trends over time
func (pa *PerformanceAnalyzer) AnalyzeTrend(current, previous *CollectionMetrics) string {
	if previous == nil || current == nil {
		return "insufficient_data"
	}

	// Check if performance is degrading
	latencyChange := float64(current.P95Latency-previous.P95Latency) / float64(previous.P95Latency)
	recallChange := (current.MeanRecall - previous.MeanRecall) / previous.MeanRecall

	if latencyChange > 0.1 && recallChange < -0.05 {
		return "degrading" // Latency up, recall down
	} else if latencyChange < -0.1 && recallChange > 0.05 {
		return "improving" // Latency down, recall up
	} else if latencyChange > 0.2 {
		return "latency_degrading"
	} else if recallChange < -0.1 {
		return "recall_degrading"
	}

	return "stable"
}
