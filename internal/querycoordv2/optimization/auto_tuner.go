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
	"context"
	"sync"
	"time"

	"github.com/milvus-io/milvus/internal/querycoordv2/task"
	"github.com/milvus-io/milvus/pkg/log"
	"github.com/milvus-io/milvus/pkg/util/paramtable"
	"go.uber.org/zap"
)

// AutoTuner automatically tunes index parameters based on metrics
// Implements the Checker interface from querycoordv2
type AutoTuner struct {
	mu sync.RWMutex

	// Core components
	metricsCollector    *MetricsCollector
	performanceAnalyzer *PerformanceAnalyzer
	decisionEngine      *DecisionEngine

	// Configuration
	enabled            bool
	checkInterval      time.Duration
	metricsWindow      time.Duration
	minSamples         int

	// State
	lastCheck          time.Time
	suggestions        map[int64]*OptimizationSuggestion // collectionID -> suggestion
	appliedChanges     []ChangeRecord                    // History of applied changes

	// Integration with QueryCoordV2 (will be populated when integrated)
	// meta               *meta.Meta
	// broker             meta.Broker
}

// ChangeRecord tracks applied parameter changes
type ChangeRecord struct {
	CollectionID       int64
	Timestamp          time.Time
	Action             OptimizationAction
	OldParams          map[string]interface{}
	NewParams          map[string]interface{}
	ExpectedImprovement map[string]float64
	ActualImprovement  map[string]float64
	Success            bool
	RolledBack         bool
}

// NewAutoTuner creates a new auto tuner
func NewAutoTuner() *AutoTuner {
	params := paramtable.Get()

	// Configuration from paramtable (with defaults)
	checkInterval := 24 * time.Hour  // Check once per day
	metricsWindow := 24 * time.Hour  // Analyze last 24 hours
	retentionPeriod := 7 * 24 * time.Hour // Keep 7 days of metrics
	maxSamples := 10000
	minSamples := 100

	return &AutoTuner{
		metricsCollector:    NewMetricsCollector(retentionPeriod, maxSamples),
		performanceAnalyzer: NewPerformanceAnalyzer(),
		decisionEngine:      NewDecisionEngine(minSamples),
		enabled:             params.QueryCoordCfg.EnableAutoTuning.GetAsBool(),
		checkInterval:       checkInterval,
		metricsWindow:       metricsWindow,
		minSamples:          minSamples,
		suggestions:         make(map[int64]*OptimizationSuggestion),
		appliedChanges:      make([]ChangeRecord, 0),
	}
}

// ID returns the checker ID
func (at *AutoTuner) ID() int {
	// Return a unique ID for this checker
	// This would typically be defined in utils.CheckerType
	return 100 // Placeholder - should be added to checker type constants
}

// Description returns the checker description
func (at *AutoTuner) Description() string {
	return "auto tuner for index parameters"
}

// IsActive returns whether the auto tuner is active
func (at *AutoTuner) IsActive() bool {
	at.mu.RLock()
	defer at.mu.RUnlock()
	return at.enabled
}

// Activate activates the auto tuner
func (at *AutoTuner) Activate() {
	at.mu.Lock()
	defer at.mu.Unlock()
	at.enabled = true
	log.Info("auto tuner activated")
}

// Deactivate deactivates the auto tuner
func (at *AutoTuner) Deactivate() {
	at.mu.Lock()
	defer at.mu.Unlock()
	at.enabled = false
	log.Info("auto tuner deactivated")
}

// Check performs periodic optimization checks
// Implements the Checker interface
func (at *AutoTuner) Check(ctx context.Context) []task.Task {
	if !at.IsActive() {
		return nil
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	// Check if it's time to run
	now := time.Now()
	if now.Sub(at.lastCheck) < at.checkInterval {
		return nil
	}

	at.lastCheck = now

	log.Info("auto tuner check started",
		zap.Time("timestamp", now))

	// Clean up old metrics
	at.metricsCollector.CleanupOldMetrics()

	// Get all collections with metrics
	collectionIDs := at.metricsCollector.GetAllCollectionIDs()
	log.Info("checking collections for optimization",
		zap.Int("count", len(collectionIDs)))

	// Analyze each collection
	var tasks []task.Task
	for _, collectionID := range collectionIDs {
		// Get aggregated metrics
		metrics := at.metricsCollector.GetCollectionMetrics(collectionID, at.metricsWindow)
		if metrics == nil {
			continue
		}

		// Analyze performance
		analysis := at.performanceAnalyzer.Analyze(metrics)
		if analysis == nil || !analysis.NeedsOptimization {
			continue
		}

		// Generate suggestion
		suggestion := at.decisionEngine.GenerateSuggestion(analysis)
		if suggestion == nil {
			continue
		}

		// Store suggestion
		at.suggestions[collectionID] = suggestion

		log.Info("optimization opportunity detected",
			zap.Int64("collectionID", collectionID),
			zap.String("action", string(suggestion.Action)),
			zap.String("reason", suggestion.Reason),
			zap.Float64("confidence", suggestion.Confidence))

		// TODO: Create actual optimization tasks
		// For Phase 1 (Monitoring), we just log suggestions
		// In future phases, this will create ParameterUpdateTask instances
	}

	return tasks
}

// RecordQueryMetrics records metrics for a query
// This should be called from query execution path
func (at *AutoTuner) RecordQueryMetrics(metrics QueryMetrics) {
	if !at.IsActive() {
		return
	}

	at.metricsCollector.RecordQuery(metrics)
}

// GetSuggestion returns the current suggestion for a collection
func (at *AutoTuner) GetSuggestion(collectionID int64) *OptimizationSuggestion {
	at.mu.RLock()
	defer at.mu.RUnlock()

	if suggestion, ok := at.suggestions[collectionID]; ok {
		return suggestion
	}
	return nil
}

// GetAllSuggestions returns all current suggestions
func (at *AutoTuner) GetAllSuggestions() map[int64]*OptimizationSuggestion {
	at.mu.RLock()
	defer at.mu.RUnlock()

	// Return a copy to avoid concurrent modification
	suggestions := make(map[int64]*OptimizationSuggestion, len(at.suggestions))
	for k, v := range at.suggestions {
		suggestions[k] = v
	}
	return suggestions
}

// GetChangeHistory returns the history of applied changes
func (at *AutoTuner) GetChangeHistory() []ChangeRecord {
	at.mu.RLock()
	defer at.mu.RUnlock()

	// Return a copy
	history := make([]ChangeRecord, len(at.appliedChanges))
	copy(history, at.appliedChanges)
	return history
}

// ApplySuggestion applies an optimization suggestion
// This will be implemented in Phase 3 (Safe parameter updates)
func (at *AutoTuner) ApplySuggestion(collectionID int64) error {
	at.mu.Lock()
	defer at.mu.Unlock()

	suggestion := at.suggestions[collectionID]
	if suggestion == nil {
		log.Warn("no suggestion to apply",
			zap.Int64("collectionID", collectionID))
		return nil
	}

	// TODO: Implement in Phase 3
	// 1. Create parameter update job
	// 2. Apply changes
	// 3. Monitor results
	// 4. Record change

	log.Info("suggestion application not yet implemented (Phase 3)",
		zap.Int64("collectionID", collectionID),
		zap.String("action", string(suggestion.Action)))

	return nil
}

// SetPerformanceTarget sets custom performance targets for a collection
func (at *AutoTuner) SetPerformanceTarget(collectionID int64, target PerformanceTarget) {
	at.mu.Lock()
	defer at.mu.Unlock()

	at.performanceAnalyzer.SetTarget(collectionID, target)

	log.Info("performance target set",
		zap.Int64("collectionID", collectionID),
		zap.Duration("targetLatency", target.TargetLatency),
		zap.Float64("targetRecall", target.TargetRecall))
}

// GetMetrics returns current metrics for a collection
func (at *AutoTuner) GetMetrics(collectionID int64) *CollectionMetrics {
	return at.metricsCollector.GetCollectionMetrics(collectionID, at.metricsWindow)
}

// GetAnalysis returns performance analysis for a collection
func (at *AutoTuner) GetAnalysis(collectionID int64) *PerformanceAnalysis {
	metrics := at.GetMetrics(collectionID)
	if metrics == nil {
		return nil
	}
	return at.performanceAnalyzer.Analyze(metrics)
}
