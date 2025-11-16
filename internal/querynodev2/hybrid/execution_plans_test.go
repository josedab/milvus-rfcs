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
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/milvus-io/milvus/internal/proto/internalpb"
	"github.com/milvus-io/milvus/internal/querynodev2/segments"
)

func TestNewFilterThenSearchPlan(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := NewFilterThenSearchPlan(req, nil)
	assert.NotNil(t, plan)
	assert.Equal(t, PlanFilterThenSearch, plan.PlanType())
}

func TestFilterThenSearchPlan_EstimatedCost(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  make([]segments.Segment, 5),
		GrowingSegments: make([]segments.Segment, 3),
	}

	plan := NewFilterThenSearchPlan(req, nil)
	cost := plan.EstimatedCost()

	// Cost should be based on total segments
	expectedCost := float64(8) * 1.0
	assert.Equal(t, expectedCost, cost)
}

func TestNewSearchThenFilterPlan(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := NewSearchThenFilterPlan(req, nil)
	assert.NotNil(t, plan)
	assert.Equal(t, PlanSearchThenFilter, plan.PlanType())
}

func TestSearchThenFilterPlan_EstimatedCost(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  make([]segments.Segment, 5),
		GrowingSegments: make([]segments.Segment, 3),
	}

	plan := NewSearchThenFilterPlan(req, nil)
	cost := plan.EstimatedCost()

	// Cost should be slightly higher than FilterThenSearch
	expectedCost := float64(8) * 1.2
	assert.Equal(t, expectedCost, cost)
}

func TestNewParallelHybridPlan(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := NewParallelHybridPlan(req, nil)
	assert.NotNil(t, plan)
	assert.Equal(t, PlanParallelHybrid, plan.PlanType())
}

func TestParallelHybridPlan_EstimatedCost(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  make([]segments.Segment, 5),
		GrowingSegments: make([]segments.Segment, 3),
	}

	plan := NewParallelHybridPlan(req, nil)
	cost := plan.EstimatedCost()

	// Cost should be lower due to parallelism
	expectedCost := float64(8) * 0.8
	assert.Equal(t, expectedCost, cost)
}

func TestAllPlans_CostOrdering(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  make([]segments.Segment, 10),
		GrowingSegments: make([]segments.Segment, 5),
	}

	filterThenSearch := NewFilterThenSearchPlan(req, nil)
	searchThenFilter := NewSearchThenFilterPlan(req, nil)
	parallelHybrid := NewParallelHybridPlan(req, nil)

	// Parallel should be cheapest
	assert.True(t, parallelHybrid.EstimatedCost() < filterThenSearch.EstimatedCost())
	assert.True(t, parallelHybrid.EstimatedCost() < searchThenFilter.EstimatedCost())

	// FilterThenSearch should be cheaper than SearchThenFilter
	assert.True(t, filterThenSearch.EstimatedCost() < searchThenFilter.EstimatedCost())
}

func TestPlanType_Interface(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plans := []ExecutionPlan{
		NewFilterThenSearchPlan(req, nil),
		NewSearchThenFilterPlan(req, nil),
		NewParallelHybridPlan(req, nil),
	}

	for _, plan := range plans {
		assert.NotNil(t, plan.PlanType())
		assert.True(t, plan.EstimatedCost() > 0)
	}
}

func TestExecutionPlan_EmptySegments(t *testing.T) {
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID: 1,
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plans := []ExecutionPlan{
		NewFilterThenSearchPlan(req, nil),
		NewSearchThenFilterPlan(req, nil),
		NewParallelHybridPlan(req, nil),
	}

	// All plans should handle empty segments gracefully
	for _, plan := range plans {
		cost := plan.EstimatedCost()
		assert.Equal(t, 0.0, cost)
	}
}
