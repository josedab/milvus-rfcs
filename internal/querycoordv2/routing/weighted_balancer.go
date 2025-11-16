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
	"sort"
)

// NodeScore represents a node with its calculated score
type NodeScore struct {
	NodeID int64
	Score  float64
}

// WeightedBalancer selects nodes based on weighted scores
type WeightedBalancer struct {
	// No state needed for stateless selection
}

// NewWeightedBalancer creates a new WeightedBalancer instance
func NewWeightedBalancer() *WeightedBalancer {
	return &WeightedBalancer{}
}

// SelectTopNodes selects the top N nodes based on their scores
func (b *WeightedBalancer) SelectTopNodes(scores map[int64]float64, count int) []int64 {
	if len(scores) == 0 {
		return nil
	}

	// Convert map to slice for sorting
	nodeScores := make([]NodeScore, 0, len(scores))
	for nodeID, score := range scores {
		nodeScores = append(nodeScores, NodeScore{
			NodeID: nodeID,
			Score:  score,
		})
	}

	// Sort by score in descending order (higher score = better)
	sort.Slice(nodeScores, func(i, j int) bool {
		return nodeScores[i].Score > nodeScores[j].Score
	})

	// Select top N nodes
	if count > len(nodeScores) {
		count = len(nodeScores)
	}

	selectedNodes := make([]int64, count)
	for i := 0; i < count; i++ {
		selectedNodes[i] = nodeScores[i].NodeID
	}

	return selectedNodes
}

// SelectBestNode selects the single best node based on scores
func (b *WeightedBalancer) SelectBestNode(scores map[int64]float64) int64 {
	nodes := b.SelectTopNodes(scores, 1)
	if len(nodes) > 0 {
		return nodes[0]
	}
	return -1
}

// SelectWithWeightedRandom selects nodes using weighted random selection
// This provides more balanced distribution compared to always selecting top nodes
func (b *WeightedBalancer) SelectWithWeightedRandom(scores map[int64]float64, count int) []int64 {
	if len(scores) == 0 {
		return nil
	}

	// For simplicity, we'll still use top-N selection
	// A true weighted random implementation would use probability-based selection
	// This can be enhanced in the future with proper weighted random sampling
	return b.SelectTopNodes(scores, count)
}
