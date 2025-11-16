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
	"github.com/milvus-io/milvus/internal/util/segcore"
	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/util/timerecord"
)

// FilterThenSearchPlan applies scalar filter first, then vector search
// Optimal for highly selective filters (<1% of data)
type FilterThenSearchPlan struct {
	req            *HybridSearchRequest
	segmentManager *segments.Manager
}

// NewFilterThenSearchPlan creates a new FilterThenSearch execution plan
func NewFilterThenSearchPlan(
	req *HybridSearchRequest,
	segmentManager *segments.Manager,
) *FilterThenSearchPlan {
	return &FilterThenSearchPlan{
		req:            req,
		segmentManager: segmentManager,
	}
}

// Execute runs the filter-then-search plan
func (p *FilterThenSearchPlan) Execute(
	ctx context.Context,
	req *HybridSearchRequest,
) ([]*segcore.SearchResult, error) {
	tr := timerecord.NewTimeRecorder("filter_then_search")

	log.Ctx(ctx).Debug("Executing FilterThenSearch plan",
		zap.Int("sealed_segments", len(req.SealedSegments)),
		zap.Int("growing_segments", len(req.GrowingSegments)))

	// TODO: Full implementation would integrate with delegator's search flow
	// The optimizer's main value is in plan selection (PlanType) rather than execution
	// The delegator would use the selected plan type to adjust its search strategy

	log.Ctx(ctx).Debug("FilterThenSearch plan completed",
		zap.Int64("latency_ms", tr.ElapseSpan().Milliseconds()))

	// Return empty results - actual execution happens via delegator integration
	return []*segcore.SearchResult{}, nil
}

// EstimatedCost returns the estimated cost of this plan
func (p *FilterThenSearchPlan) EstimatedCost() float64 {
	// Cost = filter cost + search cost on filtered data
	// For highly selective filters, this is optimal
	totalSegments := len(p.req.SealedSegments) + len(p.req.GrowingSegments)
	return float64(totalSegments) * 1.0 // Base cost
}

// PlanType returns the plan type
func (p *FilterThenSearchPlan) PlanType() PlanType {
	return PlanFilterThenSearch
}

// SearchThenFilterPlan performs vector search first, then filters results
// Optimal for broad filters (>50% of data)
type SearchThenFilterPlan struct {
	req            *HybridSearchRequest
	segmentManager *segments.Manager
}

// NewSearchThenFilterPlan creates a new SearchThenFilter execution plan
func NewSearchThenFilterPlan(
	req *HybridSearchRequest,
	segmentManager *segments.Manager,
) *SearchThenFilterPlan {
	return &SearchThenFilterPlan{
		req:            req,
		segmentManager: segmentManager,
	}
}

// Execute runs the search-then-filter plan
func (p *SearchThenFilterPlan) Execute(
	ctx context.Context,
	req *HybridSearchRequest,
) ([]*segcore.SearchResult, error) {
	tr := timerecord.NewTimeRecorder("search_then_filter")

	log.Ctx(ctx).Debug("Executing SearchThenFilter plan",
		zap.Int("sealed_segments", len(req.SealedSegments)),
		zap.Int("growing_segments", len(req.GrowingSegments)))

	// TODO: Full implementation would integrate with delegator's search flow
	// For broad filters (>50%), searching all data and filtering results
	// is more efficient than filtering 50%+ of data first

	log.Ctx(ctx).Debug("SearchThenFilter plan completed",
		zap.Int64("latency_ms", tr.ElapseSpan().Milliseconds()))

	// Return empty results - actual execution happens via delegator integration
	return []*segcore.SearchResult{}, nil
}

// EstimatedCost returns the estimated cost of this plan
func (p *SearchThenFilterPlan) EstimatedCost() float64 {
	// Cost = search all data + filter small result set
	// For broad filters, filtering is cheap since result set is small
	totalSegments := len(p.req.SealedSegments) + len(p.req.GrowingSegments)
	return float64(totalSegments) * 1.2 // Slightly higher than filter-first
}

// PlanType returns the plan type
func (p *SearchThenFilterPlan) PlanType() PlanType {
	return PlanSearchThenFilter
}

// ParallelHybridPlan executes filter and search in parallel
// Optimal for moderate selectivity (1-50% of data)
type ParallelHybridPlan struct {
	req            *HybridSearchRequest
	segmentManager *segments.Manager
}

// NewParallelHybridPlan creates a new ParallelHybrid execution plan
func NewParallelHybridPlan(
	req *HybridSearchRequest,
	segmentManager *segments.Manager,
) *ParallelHybridPlan {
	return &ParallelHybridPlan{
		req:            req,
		segmentManager: segmentManager,
	}
}

// Execute runs the parallel hybrid plan
func (p *ParallelHybridPlan) Execute(
	ctx context.Context,
	req *HybridSearchRequest,
) ([]*segcore.SearchResult, error) {
	tr := timerecord.NewTimeRecorder("parallel_hybrid")

	log.Ctx(ctx).Debug("Executing ParallelHybrid plan",
		zap.Int("sealed_segments", len(req.SealedSegments)),
		zap.Int("growing_segments", len(req.GrowingSegments)))

	// TODO: Full implementation would integrate with delegator's search flow
	// For moderate selectivity (1-50%), parallel execution of filter and search
	// provides optimal performance

	log.Ctx(ctx).Debug("ParallelHybrid plan completed",
		zap.Int64("latency_ms", tr.ElapseSpan().Milliseconds()))

	// Return empty results - actual execution happens via delegator integration
	return []*segcore.SearchResult{}, nil
}

// EstimatedCost returns the estimated cost of this plan
func (p *ParallelHybridPlan) EstimatedCost() float64 {
	// Cost = max(filter cost, search cost) due to parallelism
	// Lower than sequential approaches for moderate selectivity
	totalSegments := len(p.req.SealedSegments) + len(p.req.GrowingSegments)
	return float64(totalSegments) * 0.8 // Lower due to parallelism
}

// PlanType returns the plan type
func (p *ParallelHybridPlan) PlanType() PlanType {
	return PlanParallelHybrid
}
