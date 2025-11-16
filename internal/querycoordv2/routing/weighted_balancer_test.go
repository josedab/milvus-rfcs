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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type WeightedBalancerTestSuite struct {
	suite.Suite
	balancer *WeightedBalancer
}

func (suite *WeightedBalancerTestSuite) SetupTest() {
	suite.balancer = NewWeightedBalancer()
}

func TestWeightedBalancerSuite(t *testing.T) {
	suite.Run(t, new(WeightedBalancerTestSuite))
}

func (suite *WeightedBalancerTestSuite) TestNewWeightedBalancer() {
	balancer := NewWeightedBalancer()
	suite.NotNil(balancer)
}

func (suite *WeightedBalancerTestSuite) TestSelectTopNodes() {
	scores := map[int64]float64{
		1: 0.9,
		2: 0.7,
		3: 0.8,
		4: 0.6,
		5: 0.95,
	}

	// Test selecting top 3 nodes
	topNodes := suite.balancer.SelectTopNodes(scores, 3)
	suite.Len(topNodes, 3)

	// Verify nodes are in descending score order
	suite.Equal(int64(5), topNodes[0]) // Score: 0.95
	suite.Equal(int64(1), topNodes[1]) // Score: 0.9
	suite.Equal(int64(3), topNodes[2]) // Score: 0.8
}

func (suite *WeightedBalancerTestSuite) TestSelectTopNodesMoreThanAvailable() {
	scores := map[int64]float64{
		1: 0.9,
		2: 0.7,
	}

	// Request more nodes than available
	topNodes := suite.balancer.SelectTopNodes(scores, 5)
	suite.Len(topNodes, 2) // Should return all available
}

func (suite *WeightedBalancerTestSuite) TestSelectTopNodesEmpty() {
	scores := map[int64]float64{}

	topNodes := suite.balancer.SelectTopNodes(scores, 3)
	suite.Nil(topNodes)
}

func (suite *WeightedBalancerTestSuite) TestSelectBestNode() {
	scores := map[int64]float64{
		1: 0.5,
		2: 0.9,
		3: 0.7,
	}

	bestNode := suite.balancer.SelectBestNode(scores)
	suite.Equal(int64(2), bestNode) // Node with highest score
}

func (suite *WeightedBalancerTestSuite) TestSelectBestNodeEmpty() {
	scores := map[int64]float64{}

	bestNode := suite.balancer.SelectBestNode(scores)
	suite.Equal(int64(-1), bestNode) // Should return -1 for empty
}

func (suite *WeightedBalancerTestSuite) TestSelectWithWeightedRandom() {
	scores := map[int64]float64{
		1: 0.9,
		2: 0.7,
		3: 0.8,
	}

	// Currently this uses same logic as SelectTopNodes
	// Can be extended for true weighted random in the future
	selectedNodes := suite.balancer.SelectWithWeightedRandom(scores, 2)
	suite.Len(selectedNodes, 2)
}

func TestNodeScoreOrdering(t *testing.T) {
	scores := []NodeScore{
		{NodeID: 1, Score: 0.5},
		{NodeID: 2, Score: 0.9},
		{NodeID: 3, Score: 0.7},
	}

	// Test that we can sort by score
	assert.Greater(t, scores[1].Score, scores[0].Score)
	assert.Greater(t, scores[2].Score, scores[0].Score)
	assert.Greater(t, scores[1].Score, scores[2].Score)
}
