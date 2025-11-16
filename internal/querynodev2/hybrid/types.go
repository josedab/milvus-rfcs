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

	"github.com/milvus-io/milvus/internal/proto/internalpb"
	"github.com/milvus-io/milvus/internal/querynodev2/segments"
	"github.com/milvus-io/milvus/internal/util/segcore"
)

// ExecutionPlan represents a strategy for executing hybrid search
type ExecutionPlan interface {
	// Execute runs the hybrid search using this plan
	Execute(ctx context.Context, req *HybridSearchRequest) ([]*segcore.SearchResult, error)

	// EstimatedCost returns the estimated cost of executing this plan
	EstimatedCost() float64

	// PlanType returns the type of this execution plan
	PlanType() PlanType
}

// PlanType represents the type of execution plan
type PlanType int

const (
	// PlanFilterThenSearch applies filter first, then vector search
	PlanFilterThenSearch PlanType = iota

	// PlanSearchThenFilter performs vector search first, then filters results
	PlanSearchThenFilter

	// PlanParallelHybrid executes filter and search in parallel
	PlanParallelHybrid
)

func (t PlanType) String() string {
	switch t {
	case PlanFilterThenSearch:
		return "FilterThenSearch"
	case PlanSearchThenFilter:
		return "SearchThenFilter"
	case PlanParallelHybrid:
		return "ParallelHybrid"
	default:
		return "Unknown"
	}
}

// HybridSearchRequest encapsulates a hybrid search request
type HybridSearchRequest struct {
	// Original search request
	SearchRequest *internalpb.SearchRequest

	// Segments to search (sealed + growing)
	SealedSegments []segments.Segment
	GrowingSegments []segments.Segment

	// Collection schema for type information
	Schema *segments.CollectionSchema
}

// Predicate represents a single filter predicate
type Predicate struct {
	Field    string
	Operator string
	Value    interface{}
	Values   []string // For IN operator
}

// FilterExpression represents a parsed filter expression
type FilterExpression struct {
	Operator   string       // AND, OR, or empty for leaf predicates
	Predicates []Predicate  // Leaf predicates
	Children   []*FilterExpression // Sub-expressions
}

// FieldStats contains statistical information about a field
type FieldStats struct {
	// Number of distinct values
	Cardinality int64

	// Value frequency distribution
	Distribution map[string]int64

	// Min and max values for numeric/comparable fields
	Min interface{}
	Max interface{}

	// Total number of rows
	TotalCount int64

	// Field data type
	DataType int32
}

// CollectionStats contains statistics for all fields in a collection
type CollectionStats struct {
	FieldStats map[string]*FieldStats
	TotalRows  int64
}

// OptimizerConfig holds configuration for the optimizer
type OptimizerConfig struct {
	// SelectivityThresholds define when to use different plans
	HighlySelectiveThreshold float64 // < this value: FilterThenSearch
	BroadFilterThreshold     float64 // > this value: SearchThenFilter

	// EnableParallelExecution enables parallel hybrid plan
	EnableParallelExecution bool

	// DefaultSelectivity used when statistics are unavailable
	DefaultSelectivity float64
}

// DefaultOptimizerConfig returns default optimizer configuration
func DefaultOptimizerConfig() *OptimizerConfig {
	return &OptimizerConfig{
		HighlySelectiveThreshold: 0.01,  // 1%
		BroadFilterThreshold:     0.50,  // 50%
		EnableParallelExecution:  true,
		DefaultSelectivity:       0.50,  // Assume 50% if unknown
	}
}
