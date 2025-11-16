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
	"errors"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/internal/proto/querypb"
	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/util/typeutil"
)

// NodeMetrics holds comprehensive metrics for a QueryNode
type NodeMetrics struct {
	NodeID       int64
	LatencyP95   float64   // milliseconds
	LatencyP99   float64   // milliseconds
	MemoryUsage  float64   // 0.0 - 1.0
	CPUUsage     float64   // 0.0 - 1.0
	CacheHitRate float64   // 0.0 - 1.0
	QPS          int64     // queries per second
	ActiveQueries int64    // currently executing queries

	// Segment locality
	LocalSegments map[int64]bool

	// Historical performance
	LastUpdateTime time.Time
	HealthScore    float64  // Computed score 0.0-1.0
}

// RouterConfig contains configuration parameters for adaptive routing
type RouterConfig struct {
	// Scoring weights (must sum to 1.0)
	CPUWeight        float64  // default: 0.3
	MemoryWeight     float64  // default: 0.2
	CacheWeight      float64  // default: 0.3
	LatencyWeight    float64  // default: 0.2

	// Thresholds
	MaxCPUUsage      float64  // default: 0.9 (reject if above)
	MaxMemoryUsage   float64  // default: 0.85
	MinHealthScore   float64  // default: 0.3

	// Load balancing
	RebalanceInterval    time.Duration  // default: 30s
	MetricsUpdateInterval time.Duration  // default: 5s
}

// DefaultRouterConfig returns a configuration with recommended default values
func DefaultRouterConfig() *RouterConfig {
	return &RouterConfig{
		CPUWeight:             0.3,
		MemoryWeight:          0.2,
		CacheWeight:           0.3,
		LatencyWeight:         0.2,
		MaxCPUUsage:           0.9,
		MaxMemoryUsage:        0.85,
		MinHealthScore:        0.3,
		RebalanceInterval:     30 * time.Second,
		MetricsUpdateInterval: 5 * time.Second,
	}
}

// AdaptiveRouter implements intelligent query routing based on real-time metrics
type AdaptiveRouter struct {
	mu           sync.RWMutex
	nodeMetrics  map[int64]*NodeMetrics
	localityMap  map[int64][]int64  // segment -> preferred nodes
	loadBalancer *WeightedBalancer
	config       *RouterConfig

	// Metrics collection
	metricsUpdateInterval time.Duration
	lastUpdate            time.Time
}

// NewAdaptiveRouter creates a new AdaptiveRouter instance
func NewAdaptiveRouter(config *RouterConfig) *AdaptiveRouter {
	if config == nil {
		config = DefaultRouterConfig()
	}

	return &AdaptiveRouter{
		nodeMetrics:           make(map[int64]*NodeMetrics),
		localityMap:           make(map[int64][]int64),
		loadBalancer:          NewWeightedBalancer(),
		config:                config,
		metricsUpdateInterval: config.MetricsUpdateInterval,
		lastUpdate:            time.Now(),
	}
}

// UpdateNodeMetrics updates the metrics for a specific node
func (r *AdaptiveRouter) UpdateNodeMetrics(nodeID int64, metrics *NodeMetrics) {
	r.mu.Lock()
	defer r.mu.Unlock()

	metrics.LastUpdateTime = time.Now()
	metrics.HealthScore = r.calculateHealthScore(metrics)
	r.nodeMetrics[nodeID] = metrics

	// Update locality map
	for segmentID := range metrics.LocalSegments {
		if _, exists := r.localityMap[segmentID]; !exists {
			r.localityMap[segmentID] = make([]int64, 0)
		}
		r.localityMap[segmentID] = append(r.localityMap[segmentID], nodeID)
	}

	log.Debug("Updated node metrics",
		zap.Int64("nodeID", nodeID),
		zap.Float64("cpu", metrics.CPUUsage),
		zap.Float64("memory", metrics.MemoryUsage),
		zap.Float64("healthScore", metrics.HealthScore))
}

// RemoveNode removes a node from the routing table
func (r *AdaptiveRouter) RemoveNode(nodeID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodeMetrics, nodeID)

	// Clean up locality map
	for segmentID, nodes := range r.localityMap {
		filtered := make([]int64, 0, len(nodes))
		for _, nid := range nodes {
			if nid != nodeID {
				filtered = append(filtered, nid)
			}
		}
		if len(filtered) > 0 {
			r.localityMap[segmentID] = filtered
		} else {
			delete(r.localityMap, segmentID)
		}
	}
}

// RouteQuery selects optimal QueryNodes for a search request
func (r *AdaptiveRouter) RouteQuery(
	ctx context.Context,
	req *querypb.SearchRequest,
) ([]int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Step 1: Get candidate nodes that have required segments
	candidateNodes := r.findNodesWithSegments(req.GetReq().GetSegmentIDs())
	if len(candidateNodes) == 0 {
		return nil, errors.New("no nodes available for segments")
	}

	// Step 2: Score each candidate node
	scores := make(map[int64]float64)
	for _, nodeID := range candidateNodes {
		metrics, exists := r.nodeMetrics[nodeID]
		if !exists {
			continue
		}

		// Skip unhealthy nodes
		if !r.isNodeHealthy(metrics) {
			log.Debug("Skipping unhealthy node",
				zap.Int64("nodeID", nodeID),
				zap.Float64("cpu", metrics.CPUUsage),
				zap.Float64("memory", metrics.MemoryUsage),
				zap.Float64("healthScore", metrics.HealthScore))
			continue
		}

		// Calculate weighted score
		score := r.calculateNodeScore(metrics, req)
		scores[nodeID] = score
	}

	if len(scores) == 0 {
		return nil, errors.New("no healthy nodes available")
	}

	// Step 3: Select top nodes using weighted load balancing
	numNodes := 1
	if req.GetReq().GetNq() > 10 {
		numNodes = int(math.Min(float64(len(scores)), 3))
	}
	selectedNodes := r.loadBalancer.SelectTopNodes(scores, numNodes)

	log.Debug("Adaptive routing decision",
		zap.Int("candidates", len(candidateNodes)),
		zap.Int("selected", len(selectedNodes)),
		zap.Any("scores", scores))

	return selectedNodes, nil
}

// findNodesWithSegments returns nodes that have the required segments
func (r *AdaptiveRouter) findNodesWithSegments(segmentIDs []int64) []int64 {
	if len(segmentIDs) == 0 {
		// If no specific segments requested, return all nodes
		nodes := make([]int64, 0, len(r.nodeMetrics))
		for nodeID := range r.nodeMetrics {
			nodes = append(nodes, nodeID)
		}
		return nodes
	}

	// Find nodes that have at least one of the required segments
	nodeSet := typeutil.NewUniqueSet()
	for _, segmentID := range segmentIDs {
		if nodes, exists := r.localityMap[segmentID]; exists {
			for _, nodeID := range nodes {
				nodeSet.Insert(nodeID)
			}
		}
	}

	return nodeSet.Collect()
}

// isNodeHealthy checks if a node meets health thresholds
func (r *AdaptiveRouter) isNodeHealthy(metrics *NodeMetrics) bool {
	if metrics.CPUUsage > r.config.MaxCPUUsage {
		return false
	}
	if metrics.MemoryUsage > r.config.MaxMemoryUsage {
		return false
	}
	if metrics.HealthScore < r.config.MinHealthScore {
		return false
	}

	// Check if metrics are stale (older than 30 seconds)
	if time.Since(metrics.LastUpdateTime) > 30*time.Second {
		return false
	}

	return true
}

// calculateNodeScore computes weighted score for node selection
func (r *AdaptiveRouter) calculateNodeScore(
	metrics *NodeMetrics,
	req *querypb.SearchRequest,
) float64 {
	score := 0.0

	// Component 1: CPU headroom (lower usage = higher score)
	cpuScore := (1.0 - metrics.CPUUsage) * r.config.CPUWeight
	score += cpuScore

	// Component 2: Memory headroom
	memoryScore := (1.0 - metrics.MemoryUsage) * r.config.MemoryWeight
	score += memoryScore

	// Component 3: Cache locality (do we have these segments cached?)
	cacheScore := r.calculateCacheLocality(metrics, req.GetReq().GetSegmentIDs())
	score += cacheScore * r.config.CacheWeight

	// Component 4: Historical performance (inverse of latency)
	latencyScore := 0.0
	if metrics.LatencyP95 > 0 {
		// Normalize: 10ms = 1.0, 100ms = 0.1
		latencyScore = (10.0 / metrics.LatencyP95) * r.config.LatencyWeight
		latencyScore = math.Min(latencyScore, r.config.LatencyWeight) // Cap at weight
	}
	score += latencyScore

	return score
}

// calculateCacheLocality estimates cache hit probability
func (r *AdaptiveRouter) calculateCacheLocality(
	metrics *NodeMetrics,
	segmentIDs []int64,
) float64 {
	if len(segmentIDs) == 0 {
		// If no segments specified, use node's overall cache hit rate
		return metrics.CacheHitRate
	}

	localCount := 0
	for _, segID := range segmentIDs {
		if metrics.LocalSegments[segID] {
			localCount++
		}
	}

	// Return proportion of segments available locally
	return float64(localCount) / float64(len(segmentIDs))
}

// calculateHealthScore computes overall health score for a node
func (r *AdaptiveRouter) calculateHealthScore(metrics *NodeMetrics) float64 {
	score := 0.0

	// CPU headroom
	score += (1.0 - metrics.CPUUsage) * 0.3

	// Memory headroom
	score += (1.0 - metrics.MemoryUsage) * 0.2

	// Cache performance
	score += metrics.CacheHitRate * 0.3

	// Latency performance
	if metrics.LatencyP95 > 0 {
		latencyScore := 10.0 / metrics.LatencyP95
		score += math.Min(latencyScore, 0.2)
	}

	return score
}

// GetNodeMetrics returns metrics for a specific node
func (r *AdaptiveRouter) GetNodeMetrics(nodeID int64) (*NodeMetrics, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics, exists := r.nodeMetrics[nodeID]
	return metrics, exists
}

// GetAllNodeMetrics returns metrics for all nodes
func (r *AdaptiveRouter) GetAllNodeMetrics() map[int64]*NodeMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[int64]*NodeMetrics, len(r.nodeMetrics))
	for nodeID, metrics := range r.nodeMetrics {
		// Return a copy to prevent external modifications
		metricsCopy := *metrics
		result[nodeID] = &metricsCopy
	}

	return result
}
