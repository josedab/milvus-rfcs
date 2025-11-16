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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/milvus-io/milvus/internal/proto/internalpb"
)

func TestNewSelectivityEstimator(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	assert.NotNil(t, estimator)
	assert.NotNil(t, estimator.statsCache)
	assert.NotNil(t, estimator.config)
}

func TestEstimateSelectivity_NoStats(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID:       1,
			SerializedExprPlan: make([]byte, 100),
		},
	}

	selectivity := estimator.EstimateSelectivity(context.Background(), req)
	assert.Equal(t, config.DefaultSelectivity, selectivity)
}

func TestEstimateSelectivity_NoFilter(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &CollectionStats{
		TotalRows: 1000,
	}
	cache.Update(1, stats)

	req := &HybridSearchRequest{
		SearchRequest: &internalpb.SearchRequest{
			CollectionID:       1,
			SerializedExprPlan: nil,
		},
	}

	selectivity := estimator.EstimateSelectivity(context.Background(), req)
	assert.Equal(t, 1.0, selectivity)
}

func TestEstimateFromExpressionComplexity(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &CollectionStats{
		TotalRows: 1000,
	}

	// Very small expression (highly selective)
	expr1 := make([]byte, 30)
	sel1 := estimator.estimateFromExpressionComplexity(expr1, stats)
	assert.Equal(t, 0.001, sel1)

	// Small expression (selective)
	expr2 := make([]byte, 80)
	sel2 := estimator.estimateFromExpressionComplexity(expr2, stats)
	assert.Equal(t, 0.01, sel2)

	// Medium expression (moderate)
	expr3 := make([]byte, 150)
	sel3 := estimator.estimateFromExpressionComplexity(expr3, stats)
	assert.Equal(t, 0.10, sel3)

	// Large expression (less selective)
	expr4 := make([]byte, 300)
	sel4 := estimator.estimateFromExpressionComplexity(expr4, stats)
	assert.Equal(t, 0.30, sel4)

	// Very large expression (broad)
	expr5 := make([]byte, 500)
	sel5 := estimator.estimateFromExpressionComplexity(expr5, stats)
	assert.Equal(t, 0.60, sel5)
}

func TestEstimatePredicate_Equality(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
	}

	pred := Predicate{
		Field:    "category",
		Operator: "=",
		Value:    "electronics",
	}

	selectivity := estimator.estimatePredicate(pred, stats)
	assert.Equal(t, 1.0/100.0, selectivity)
}

func TestEstimatePredicate_NotEqual(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
	}

	pred := Predicate{
		Field:    "category",
		Operator: "!=",
		Value:    "electronics",
	}

	selectivity := estimator.estimatePredicate(pred, stats)
	assert.Equal(t, 1.0-(1.0/100.0), selectivity)
}

func TestEstimatePredicate_In(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
		Distribution: map[string]int64{
			"electronics": 500,
			"home":        300,
			"sports":      200,
		},
	}

	pred := Predicate{
		Field:    "category",
		Operator: "IN",
		Values:   []string{"electronics", "home"},
	}

	selectivity := estimator.estimatePredicate(pred, stats)
	expected := (500.0 + 300.0) / 1000.0
	assert.Equal(t, expected, selectivity)
}

func TestEstimatePredicate_NotIn(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
		Distribution: map[string]int64{
			"electronics": 500,
			"home":        300,
			"sports":      200,
		},
	}

	pred := Predicate{
		Field:    "category",
		Operator: "NOT IN",
		Values:   []string{"electronics", "home"},
	}

	selectivity := estimator.estimatePredicate(pred, stats)
	expected := 1.0 - ((500.0 + 300.0) / 1000.0)
	assert.Equal(t, expected, selectivity)
}

func TestEstimatePredicate_Range(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
		Min:         0,
		Max:         100,
	}

	pred := Predicate{
		Field:    "price",
		Operator: ">",
		Value:    50,
	}

	selectivity := estimator.estimatePredicate(pred, stats)
	assert.Equal(t, 0.50, selectivity) // Default for range
}

func TestCombineSelectivities_And(t *testing.T) {
	selectivities := []float64{0.5, 0.3, 0.2}
	combined := combineSelectivities(selectivities, "AND")
	expected := 0.5 * 0.3 * 0.2
	assert.Equal(t, expected, combined)
}

func TestCombineSelectivities_Or(t *testing.T) {
	selectivities := []float64{0.3, 0.2}
	combined := combineSelectivities(selectivities, "OR")
	// P(A ∪ B) = 1 - (1-0.3)*(1-0.2) = 1 - 0.7*0.8 = 1 - 0.56 = 0.44
	expected := 1.0 - (1.0-0.3)*(1.0-0.2)
	assert.InDelta(t, expected, combined, 0.001)
}

func TestCombineSelectivities_Empty(t *testing.T) {
	selectivities := []float64{}
	combined := combineSelectivities(selectivities, "AND")
	assert.Equal(t, 1.0, combined)
}

func TestCombineSelectivities_Single(t *testing.T) {
	selectivities := []float64{0.5}
	combined := combineSelectivities(selectivities, "AND")
	assert.Equal(t, 0.5, combined)
}

func TestValidateSelectivity(t *testing.T) {
	assert.Equal(t, 0.0, validateSelectivity(-0.5))
	assert.Equal(t, 1.0, validateSelectivity(1.5))
	assert.Equal(t, 0.5, validateSelectivity(0.5))
	assert.Equal(t, 0.5, validateSelectivity(math.NaN()))
}

func TestEstimateRangePredicate(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
		Min:         0,
		Max:         100,
	}

	// Test different range operators
	operators := []string{">", ">=", "<", "<="}
	for _, op := range operators {
		pred := Predicate{
			Field:    "price",
			Operator: op,
			Value:    50,
		}
		selectivity := estimator.estimateRangePredicate(pred, stats)
		assert.True(t, selectivity > 0 && selectivity <= 1.0)
	}
}

func TestEstimateRangePredicate_NoMinMax(t *testing.T) {
	cache := NewStatisticsCache()
	config := DefaultOptimizerConfig()
	estimator := NewSelectivityEstimator(cache, config)

	stats := &FieldStats{
		Cardinality: 100,
		TotalCount:  1000,
		Min:         nil,
		Max:         nil,
	}

	pred := Predicate{
		Field:    "price",
		Operator: ">",
		Value:    50,
	}

	selectivity := estimator.estimateRangePredicate(pred, stats)
	assert.Equal(t, 0.33, selectivity) // Default
}
