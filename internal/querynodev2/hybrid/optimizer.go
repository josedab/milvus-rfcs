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

package hybrid

import (
	"context"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/internal/querynodev2/segments"
	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/metrics"
	"github.com/milvus-io/milvus/pkg/v2/util/timerecord"
)

// HybridSearchOptimizer selects the optimal execution plan for hybrid search
type HybridSearchOptimizer struct {
	config            *OptimizerConfig
	selectivityEst    *SelectivityEstimator
	statisticsCache   *StatisticsCache
	segmentManager    *segments.Manager
}

// NewHybridSearchOptimizer creates a new optimizer instance
func NewHybridSearchOptimizer(
	segmentManager *segments.Manager,
	config *OptimizerConfig,
) *HybridSearchOptimizer {
	if config == nil {
		config = DefaultOptimizerConfig()
	}

	statsCache := NewStatisticsCache()
	selectivityEst := NewSelectivityEstimator(statsCache, config)

	return &HybridSearchOptimizer{
		config:          config,
		selectivityEst:  selectivityEst,
		statisticsCache: statsCache,
		segmentManager:  segmentManager,
	}
}

// OptimizePlan selects the best execution plan based on filter selectivity
func (o *HybridSearchOptimizer) OptimizePlan(
	ctx context.Context,
	req *HybridSearchRequest,
) ExecutionPlan {
	tr := timerecord.NewTimeRecorder("hybrid_optimize_plan")
	defer func() {
		metrics.QueryNodeHybridSearchOptimizeLatency.WithLabelValues().Observe(float64(tr.ElapseSpan().Milliseconds()))
	}()

	// If no filter expression, use standard search
	if req.SearchRequest.GetSerializedExprPlan() == nil || len(req.SearchRequest.GetSerializedExprPlan()) == 0 {
		log.Ctx(ctx).Debug("No filter expression, using SearchThenFilter plan")
		return NewSearchThenFilterPlan(req, o.segmentManager)
	}

	// Estimate filter selectivity
	selectivity := o.selectivityEst.EstimateSelectivity(ctx, req)

	log.Ctx(ctx).Debug("Estimated filter selectivity",
		zap.Float64("selectivity", selectivity),
		zap.Int("sealed_segments", len(req.SealedSegments)),
		zap.Int("growing_segments", len(req.GrowingSegments)))

	// Record selectivity metric
	metrics.QueryNodeHybridSearchSelectivity.WithLabelValues().Observe(selectivity)

	// Select plan based on selectivity thresholds
	var plan ExecutionPlan
	var planType PlanType

	if selectivity < o.config.HighlySelectiveThreshold {
		// Highly selective filter (<1%) - filter first to reduce search space
		planType = PlanFilterThenSearch
		plan = NewFilterThenSearchPlan(req, o.segmentManager)

	} else if selectivity > o.config.BroadFilterThreshold {
		// Broad filter (>50%) - search first, filter small result set
		planType = PlanSearchThenFilter
		plan = NewSearchThenFilterPlan(req, o.segmentManager)

	} else if o.config.EnableParallelExecution {
		// Moderate selectivity (1-50%) - parallel execution
		planType = PlanParallelHybrid
		plan = NewParallelHybridPlan(req, o.segmentManager)

	} else {
		// Fallback to filter-then-search if parallel disabled
		planType = PlanFilterThenSearch
		plan = NewFilterThenSearchPlan(req, o.segmentManager)
	}

	log.Ctx(ctx).Info("Selected execution plan",
		zap.String("plan_type", planType.String()),
		zap.Float64("selectivity", selectivity),
		zap.Float64("estimated_cost", plan.EstimatedCost()))

	// Record plan selection metric
	metrics.QueryNodeHybridSearchPlanType.WithLabelValues(planType.String()).Inc()

	return plan
}

// UpdateStatistics updates cached statistics for a collection
func (o *HybridSearchOptimizer) UpdateStatistics(
	collectionID int64,
	stats *CollectionStats,
) {
	o.statisticsCache.Update(collectionID, stats)
}

// GetStatistics retrieves cached statistics for a collection
func (o *HybridSearchOptimizer) GetStatistics(collectionID int64) *CollectionStats {
	return o.statisticsCache.Get(collectionID)
}
