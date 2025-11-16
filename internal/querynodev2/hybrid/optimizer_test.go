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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/milvus-io/milvus/internal/proto/internalpb"
	"github.com/milvus-io/milvus/internal/querynodev2/segments"
)

type OptimizerTestSuite struct {
	suite.Suite
	optimizer *HybridSearchOptimizer
	config    *OptimizerConfig
}

func (suite *OptimizerTestSuite) SetupTest() {
	suite.config = DefaultOptimizerConfig()
	suite.optimizer = NewHybridSearchOptimizer(nil, suite.config)
}

func (suite *OptimizerTestSuite) TestNewHybridSearchOptimizer() {
	optimizer := NewHybridSearchOptimizer(nil, nil)
	assert.NotNil(suite.T(), optimizer)
	assert.NotNil(suite.T(), optimizer.config)
	assert.NotNil(suite.T(), optimizer.selectivityEst)
	assert.NotNil(suite.T(), optimizer.statisticsCache)
}

func (suite *OptimizerTestSuite) TestOptimizePlan_NoFilter() {
	ctx := context.Background()
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID:       1,
			SerializedExprPlan: nil, // No filter
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := suite.optimizer.OptimizePlan(ctx, req)
	assert.NotNil(suite.T(), plan)
	assert.Equal(suite.T(), PlanSearchThenFilter, plan.PlanType())
}

func (suite *OptimizerTestSuite) TestOptimizePlan_HighlySelectiveFilter() {
	ctx := context.Background()

	// Create statistics indicating highly selective filter
	stats := &CollectionStats{
		TotalRows: 1000000,
		FieldStats: map[string]*FieldStats{
			"category": {
				Cardinality: 1000,
				TotalCount:  1000000,
			},
		},
	}
	suite.optimizer.UpdateStatistics(1, stats)

	// Small expression = highly selective
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID:       1,
			SerializedExprPlan: make([]byte, 30), // Small expression
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := suite.optimizer.OptimizePlan(ctx, req)
	assert.NotNil(suite.T(), plan)
	assert.Equal(suite.T(), PlanFilterThenSearch, plan.PlanType())
}

func (suite *OptimizerTestSuite) TestOptimizePlan_BroadFilter() {
	ctx := context.Background()

	stats := &CollectionStats{
		TotalRows: 1000000,
		FieldStats: map[string]*FieldStats{
			"category": {
				Cardinality: 10,
				TotalCount:  1000000,
			},
		},
	}
	suite.optimizer.UpdateStatistics(1, stats)

	// Large expression = broad filter
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID:       1,
			SerializedExprPlan: make([]byte, 500), // Large expression
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := suite.optimizer.OptimizePlan(ctx, req)
	assert.NotNil(suite.T(), plan)
	assert.Equal(suite.T(), PlanSearchThenFilter, plan.PlanType())
}

func (suite *OptimizerTestSuite) TestOptimizePlan_ModerateSelectivity() {
	ctx := context.Background()

	stats := &CollectionStats{
		TotalRows: 1000000,
		FieldStats: map[string]*FieldStats{
			"category": {
				Cardinality: 100,
				TotalCount:  1000000,
			},
		},
	}
	suite.optimizer.UpdateStatistics(1, stats)

	// Medium expression = moderate selectivity
	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID:       1,
			SerializedExprPlan: make([]byte, 150), // Medium expression
		},
		SealedSegments:  []segments.Segment{},
		GrowingSegments: []segments.Segment{},
	}

	plan := suite.optimizer.OptimizePlan(ctx, req)
	assert.NotNil(suite.T(), plan)
	// Should select parallel plan for moderate selectivity
	assert.Equal(suite.T(), PlanParallelHybrid, plan.PlanType())
}

func (suite *OptimizerTestSuite) TestUpdateAndGetStatistics() {
	stats := &CollectionStats{
		TotalRows: 1000,
		FieldStats: map[string]*FieldStats{
			"field1": {
				Cardinality: 100,
				TotalCount:  1000,
			},
		},
	}

	suite.optimizer.UpdateStatistics(123, stats)

	retrieved := suite.optimizer.GetStatistics(123)
	assert.NotNil(suite.T(), retrieved)
	assert.Equal(suite.T(), int64(1000), retrieved.TotalRows)
	assert.Equal(suite.T(), int64(100), retrieved.FieldStats["field1"].Cardinality)
}

func (suite *OptimizerTestSuite) TestGetStatistics_NotFound() {
	retrieved := suite.optimizer.GetStatistics(999)
	assert.Nil(suite.T(), retrieved)
}

func TestOptimizerTestSuite(t *testing.T) {
	suite.Run(t, new(OptimizerTestSuite))
}

func TestDefaultOptimizerConfig(t *testing.T) {
	config := DefaultOptimizerConfig()
	assert.NotNil(t, config)
	assert.Equal(t, 0.01, config.HighlySelectiveThreshold)
	assert.Equal(t, 0.50, config.BroadFilterThreshold)
	assert.True(t, config.EnableParallelExecution)
	assert.Equal(t, 0.50, config.DefaultSelectivity)
}

func TestPlanTypeString(t *testing.T) {
	assert.Equal(t, "FilterThenSearch", PlanFilterThenSearch.String())
	assert.Equal(t, "SearchThenFilter", PlanSearchThenFilter.String())
	assert.Equal(t, "ParallelHybrid", PlanParallelHybrid.String())
	assert.Equal(t, "Unknown", PlanType(999).String())
}
