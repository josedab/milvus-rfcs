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

package routing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/milvus-io/milvus/internal/proto/querypb"
)

type AdaptiveRouterTestSuite struct {
	suite.Suite
	router *AdaptiveRouter
	config *RouterConfig
}

func (suite *AdaptiveRouterTestSuite) SetupTest() {
	suite.config = DefaultRouterConfig()
	suite.router = NewAdaptiveRouter(suite.config)
}

func TestAdaptiveRouterSuite(t *testing.T) {
	suite.Run(t, new(AdaptiveRouterTestSuite))
}

func (suite *AdaptiveRouterTestSuite) TestNewAdaptiveRouter() {
	// Test creation with nil config
	router := NewAdaptiveRouter(nil)
	suite.NotNil(router)
	suite.NotNil(router.config)
	suite.NotNil(router.loadBalancer)
	suite.NotNil(router.nodeMetrics)
	suite.NotNil(router.localityMap)

	// Test creation with custom config
	customConfig := &RouterConfig{
		CPUWeight:             0.4,
		MemoryWeight:          0.3,
		CacheWeight:           0.2,
		LatencyWeight:         0.1,
		MaxCPUUsage:           0.8,
		MaxMemoryUsage:        0.75,
		MinHealthScore:        0.4,
		MetricsUpdateInterval: 10 * time.Second,
		RebalanceInterval:     60 * time.Second,
	}
	router = NewAdaptiveRouter(customConfig)
	suite.Equal(customConfig.CPUWeight, router.config.CPUWeight)
	suite.Equal(customConfig.MaxCPUUsage, router.config.MaxCPUUsage)
}

func (suite *AdaptiveRouterTestSuite) TestUpdateNodeMetrics() {
	nodeID := int64(1)
	metrics := &NodeMetrics{
		NodeID:        nodeID,
		CPUUsage:      0.5,
		MemoryUsage:   0.6,
		CacheHitRate:  0.7,
		LatencyP95:    50.0,
		LatencyP99:    80.0,
		QPS:           1000,
		ActiveQueries: 10,
		LocalSegments: map[int64]bool{
			100: true,
			101: true,
		},
	}

	suite.router.UpdateNodeMetrics(nodeID, metrics)

	// Verify metrics were stored
	stored, exists := suite.router.GetNodeMetrics(nodeID)
	suite.True(exists)
	suite.Equal(metrics.CPUUsage, stored.CPUUsage)
	suite.Equal(metrics.MemoryUsage, stored.MemoryUsage)
	suite.NotZero(stored.HealthScore) // Health score should be calculated
	suite.NotZero(stored.LastUpdateTime)

	// Verify locality map was updated
	suite.Contains(suite.router.localityMap, int64(100))
	suite.Contains(suite.router.localityMap, int64(101))
}

func (suite *AdaptiveRouterTestSuite) TestRemoveNode() {
	nodeID := int64(1)
	metrics := &NodeMetrics{
		NodeID:       nodeID,
		CPUUsage:     0.5,
		MemoryUsage:  0.6,
		LocalSegments: map[int64]bool{100: true},
	}

	suite.router.UpdateNodeMetrics(nodeID, metrics)
	_, exists := suite.router.GetNodeMetrics(nodeID)
	suite.True(exists)

	suite.router.RemoveNode(nodeID)
	_, exists = suite.router.GetNodeMetrics(nodeID)
	suite.False(exists)

	// Verify locality map was cleaned up
	suite.NotContains(suite.router.localityMap, int64(100))
}

func (suite *AdaptiveRouterTestSuite) TestIsNodeHealthy() {
	tests := []struct {
		name        string
		metrics     *NodeMetrics
		expectHealthy bool
	}{
		{
			name: "healthy node",
			metrics: &NodeMetrics{
				CPUUsage:       0.5,
				MemoryUsage:    0.6,
				HealthScore:    0.8,
				LastUpdateTime: time.Now(),
			},
			expectHealthy: true,
		},
		{
			name: "high CPU usage",
			metrics: &NodeMetrics{
				CPUUsage:       0.95,
				MemoryUsage:    0.5,
				HealthScore:    0.8,
				LastUpdateTime: time.Now(),
			},
			expectHealthy: false,
		},
		{
			name: "high memory usage",
			metrics: &NodeMetrics{
				CPUUsage:       0.5,
				MemoryUsage:    0.9,
				HealthScore:    0.8,
				LastUpdateTime: time.Now(),
			},
			expectHealthy: false,
		},
		{
			name: "low health score",
			metrics: &NodeMetrics{
				CPUUsage:       0.5,
				MemoryUsage:    0.6,
				HealthScore:    0.2,
				LastUpdateTime: time.Now(),
			},
			expectHealthy: false,
		},
		{
			name: "stale metrics",
			metrics: &NodeMetrics{
				CPUUsage:       0.5,
				MemoryUsage:    0.6,
				HealthScore:    0.8,
				LastUpdateTime: time.Now().Add(-60 * time.Second),
			},
			expectHealthy: false,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			result := suite.router.isNodeHealthy(tt.metrics)
			suite.Equal(tt.expectHealthy, result)
		})
	}
}

func (suite *AdaptiveRouterTestSuite) TestCalculateNodeScore() {
	metrics := &NodeMetrics{
		CPUUsage:      0.3,  // 70% headroom
		MemoryUsage:   0.4,  // 60% headroom
		CacheHitRate:  0.8,
		LatencyP95:    20.0, // Good latency
		LocalSegments: map[int64]bool{
			100: true,
			101: true,
		},
	}

	req := &querypb.SearchRequest{
		Req: &querypb.SearchRequest_InternalSearchRequest{
			InternalSearchRequest: &querypb.InternalSearchRequest{
				SegmentIDs: []int64{100, 101},
				Nq:         10,
			},
		},
	}

	score := suite.router.calculateNodeScore(metrics, req)

	// Score should be positive and reasonably high for a healthy node
	suite.Greater(score, 0.0)
	suite.Less(score, 1.0) // Should not exceed 1.0 given default weights
}

func (suite *AdaptiveRouterTestSuite) TestCalculateCacheLocality() {
	metrics := &NodeMetrics{
		LocalSegments: map[int64]bool{
			100: true,
			101: true,
			102: true,
		},
		CacheHitRate: 0.75,
	}

	// Test with segments that are all local
	segmentIDs := []int64{100, 101, 102}
	locality := suite.router.calculateCacheLocality(metrics, segmentIDs)
	suite.Equal(1.0, locality)

	// Test with partial locality
	segmentIDs = []int64{100, 101, 999}
	locality = suite.router.calculateCacheLocality(metrics, segmentIDs)
	suite.InDelta(0.666, locality, 0.01)

	// Test with no locality
	segmentIDs = []int64{999, 998}
	locality = suite.router.calculateCacheLocality(metrics, segmentIDs)
	suite.Equal(0.0, locality)

	// Test with empty segment list (should use cache hit rate)
	locality = suite.router.calculateCacheLocality(metrics, nil)
	suite.Equal(0.75, locality)
}

func (suite *AdaptiveRouterTestSuite) TestCalculateHealthScore() {
	metrics := &NodeMetrics{
		CPUUsage:     0.3,
		MemoryUsage:  0.4,
		CacheHitRate: 0.8,
		LatencyP95:   20.0,
	}

	score := suite.router.calculateHealthScore(metrics)

	// Health score should be between 0 and 1
	suite.GreaterOrEqual(score, 0.0)
	suite.LessOrEqual(score, 1.0)

	// Higher values (better metrics) should give higher score
	suite.Greater(score, 0.5)
}

func (suite *AdaptiveRouterTestSuite) TestRouteQuery() {
	// Setup multiple nodes with different characteristics
	node1 := int64(1)
	node2 := int64(2)
	node3 := int64(3)

	// Node 1: Healthy, has segment 100
	suite.router.UpdateNodeMetrics(node1, &NodeMetrics{
		NodeID:        node1,
		CPUUsage:      0.3,
		MemoryUsage:   0.4,
		CacheHitRate:  0.9,
		LatencyP95:    15.0,
		LocalSegments: map[int64]bool{100: true},
	})

	// Node 2: Healthy, has segment 101
	suite.router.UpdateNodeMetrics(node2, &NodeMetrics{
		NodeID:        node2,
		CPUUsage:      0.4,
		MemoryUsage:   0.5,
		CacheHitRate:  0.7,
		LatencyP95:    25.0,
		LocalSegments: map[int64]bool{101: true},
	})

	// Node 3: Overloaded
	suite.router.UpdateNodeMetrics(node3, &NodeMetrics{
		NodeID:        node3,
		CPUUsage:      0.95,
		MemoryUsage:   0.9,
		CacheHitRate:  0.5,
		LatencyP95:    50.0,
		LocalSegments: map[int64]bool{100: true, 101: true},
	})

	ctx := context.Background()
	req := &querypb.SearchRequest{
		Req: &querypb.SearchRequest_InternalSearchRequest{
			InternalSearchRequest: &querypb.InternalSearchRequest{
				SegmentIDs: []int64{100},
				Nq:         5,
			},
		},
	}

	selectedNodes, err := suite.router.RouteQuery(ctx, req)

	suite.NoError(err)
	suite.NotEmpty(selectedNodes)
	// Node 1 should be selected (has segment, healthy, good metrics)
	suite.Contains(selectedNodes, node1)
	// Node 3 should NOT be selected (overloaded)
	suite.NotContains(selectedNodes, node3)
}

func (suite *AdaptiveRouterTestSuite) TestRouteQueryNoHealthyNodes() {
	// Setup only unhealthy nodes
	node1 := int64(1)
	suite.router.UpdateNodeMetrics(node1, &NodeMetrics{
		NodeID:       node1,
		CPUUsage:     0.95,
		MemoryUsage:  0.9,
		LocalSegments: map[int64]bool{100: true},
	})

	ctx := context.Background()
	req := &querypb.SearchRequest{
		Req: &querypb.SearchRequest_InternalSearchRequest{
			InternalSearchRequest: &querypb.InternalSearchRequest{
				SegmentIDs: []int64{100},
			},
		},
	}

	_, err := suite.router.RouteQuery(ctx, req)
	suite.Error(err)
	suite.Contains(err.Error(), "no healthy nodes available")
}

func (suite *AdaptiveRouterTestSuite) TestGetAllNodeMetrics() {
	// Add multiple nodes
	node1 := int64(1)
	node2 := int64(2)

	metrics1 := &NodeMetrics{NodeID: node1, CPUUsage: 0.5}
	metrics2 := &NodeMetrics{NodeID: node2, CPUUsage: 0.6}

	suite.router.UpdateNodeMetrics(node1, metrics1)
	suite.router.UpdateNodeMetrics(node2, metrics2)

	allMetrics := suite.router.GetAllNodeMetrics()

	suite.Len(allMetrics, 2)
	suite.Contains(allMetrics, node1)
	suite.Contains(allMetrics, node2)
}

func TestDefaultRouterConfig(t *testing.T) {
	config := DefaultRouterConfig()

	assert.NotNil(t, config)
	assert.Equal(t, 0.3, config.CPUWeight)
	assert.Equal(t, 0.2, config.MemoryWeight)
	assert.Equal(t, 0.3, config.CacheWeight)
	assert.Equal(t, 0.2, config.LatencyWeight)
	assert.Equal(t, 0.9, config.MaxCPUUsage)
	assert.Equal(t, 0.85, config.MaxMemoryUsage)
	assert.Equal(t, 0.3, config.MinHealthScore)

	// Verify weights sum to 1.0
	totalWeight := config.CPUWeight + config.MemoryWeight + config.CacheWeight + config.LatencyWeight
	assert.InDelta(t, 1.0, totalWeight, 0.001)
}
