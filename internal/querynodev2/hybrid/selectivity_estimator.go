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

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/util/typeutil"
)

// SelectivityEstimator estimates filter selectivity based on statistics
type SelectivityEstimator struct {
	statsCache *StatisticsCache
	config     *OptimizerConfig
}

// NewSelectivityEstimator creates a new selectivity estimator
func NewSelectivityEstimator(
	statsCache *StatisticsCache,
	config *OptimizerConfig,
) *SelectivityEstimator {
	return &SelectivityEstimator{
		statsCache: statsCache,
		config:     config,
	}
}

// EstimateSelectivity estimates what fraction of data passes the filter
func (e *SelectivityEstimator) EstimateSelectivity(
	ctx context.Context,
	req *HybridSearchRequest,
) float64 {
	// Get collection statistics
	collectionID := req.SearchRequest.GetCollectionID()
	stats := e.statsCache.Get(collectionID)

	// If no statistics available, return default
	if stats == nil || stats.TotalRows == 0 {
		log.Ctx(ctx).Debug("No statistics available, using default selectivity",
			zap.Int64("collection_id", collectionID),
			zap.Float64("default_selectivity", e.config.DefaultSelectivity))
		return e.config.DefaultSelectivity
	}

	// Parse filter expression from serialized plan
	// For now, use a heuristic based on expression complexity
	// In a full implementation, this would parse the actual expression plan
	exprPlan := req.SearchRequest.GetSerializedExprPlan()
	if exprPlan == nil || len(exprPlan) == 0 {
		return 1.0 // No filter means all data passes
	}

	// Heuristic estimation based on expression size and statistics
	// Smaller expressions tend to be more selective
	selectivity := e.estimateFromExpressionComplexity(exprPlan, stats)

	log.Ctx(ctx).Debug("Estimated selectivity from expression",
		zap.Float64("selectivity", selectivity),
		zap.Int("expr_size", len(exprPlan)))

	return selectivity
}

// estimateFromExpressionComplexity provides a heuristic selectivity estimate
func (e *SelectivityEstimator) estimateFromExpressionComplexity(
	exprPlan []byte,
	stats *CollectionStats,
) float64 {
	// This is a simplified heuristic implementation
	// A full implementation would parse the expression tree and evaluate
	// selectivity for each predicate using field statistics

	exprSize := len(exprPlan)

	// Heuristic: smaller expressions tend to be more selective
	// This maps expression size to selectivity
	switch {
	case exprSize < 50:
		// Very simple expression, likely highly selective
		// Example: "id = 123"
		return 0.001 // 0.1%

	case exprSize < 100:
		// Simple expression, likely selective
		// Example: "category = 'electronics' AND price > 100"
		return 0.01 // 1%

	case exprSize < 200:
		// Moderate complexity
		// Example: "category IN ['a', 'b', 'c'] AND status = 'active'"
		return 0.10 // 10%

	case exprSize < 400:
		// More complex expression
		// Example: multiple OR conditions
		return 0.30 // 30%

	default:
		// Very complex expression, likely not very selective
		return 0.60 // 60%
	}
}

// estimatePredicate estimates selectivity for a single predicate
func (e *SelectivityEstimator) estimatePredicate(
	pred Predicate,
	stats *FieldStats,
) float64 {
	if stats == nil || stats.TotalCount == 0 {
		return e.config.DefaultSelectivity
	}

	switch pred.Operator {
	case "=", "==":
		// Equality predicate: selectivity = 1 / cardinality
		if stats.Cardinality > 0 {
			return 1.0 / float64(stats.Cardinality)
		}
		return 0.001 // Default for high cardinality

	case "!=", "<>":
		// Not equal: inverse of equality
		if stats.Cardinality > 0 {
			return 1.0 - (1.0 / float64(stats.Cardinality))
		}
		return 0.999

	case ">", ">=", "<", "<=":
		// Range predicate: use histogram or estimate based on min/max
		return e.estimateRangePredicate(pred, stats)

	case "IN":
		// IN clause: sum selectivities of individual values
		if len(pred.Values) == 0 {
			return 0.0
		}

		totalSelectivity := 0.0
		for _, val := range pred.Values {
			if freq, ok := stats.Distribution[val]; ok {
				totalSelectivity += float64(freq) / float64(stats.TotalCount)
			} else if stats.Cardinality > 0 {
				// Value not in distribution, estimate
				totalSelectivity += 1.0 / float64(stats.Cardinality)
			}
		}

		// Cap at 1.0
		return math.Min(totalSelectivity, 1.0)

	case "NOT IN":
		// NOT IN: inverse of IN
		inSelectivity := e.estimatePredicate(Predicate{
			Field:    pred.Field,
			Operator: "IN",
			Values:   pred.Values,
		}, stats)
		return 1.0 - inSelectivity

	default:
		// Unknown operator
		return e.config.DefaultSelectivity
	}
}

// estimateRangePredicate estimates selectivity for range predicates
func (e *SelectivityEstimator) estimateRangePredicate(
	pred Predicate,
	stats *FieldStats,
) float64 {
	// Without histogram, use simple heuristic
	// Assume uniform distribution between min and max

	if stats.Min == nil || stats.Max == nil {
		return 0.33 // Default guess for range queries
	}

	// For numeric types, calculate selectivity based on range
	// This is simplified - full implementation would use histograms

	switch pred.Operator {
	case ">", ">=":
		// Greater than: assume 50% by default
		return 0.50

	case "<", "<=":
		// Less than: assume 50% by default
		return 0.50

	default:
		return 0.33
	}
}

// combineSelectivities combines selectivities for AND/OR operations
func combineSelectivities(selectivities []float64, operator string) float64 {
	if len(selectivities) == 0 {
		return 1.0
	}

	if len(selectivities) == 1 {
		return selectivities[0]
	}

	switch operator {
	case "AND":
		// For AND: multiply selectivities (intersection)
		result := 1.0
		for _, sel := range selectivities {
			result *= sel
		}
		return result

	case "OR":
		// For OR: use inclusion-exclusion principle
		// P(A ∪ B) = P(A) + P(B) - P(A ∩ B)
		// Simplified: 1 - ∏(1 - si)
		result := 0.0
		for _, sel := range selectivities {
			result = 1.0 - (1.0-result)*(1.0-sel)
		}
		return result

	default:
		// Unknown operator, return average
		sum := 0.0
		for _, sel := range selectivities {
			sum += sel
		}
		return sum / float64(len(selectivities))
	}
}

// EstimateFromHistogram estimates selectivity using field histograms
// This is a placeholder for future enhancement
func (e *SelectivityEstimator) EstimateFromHistogram(
	fieldName string,
	predicate Predicate,
	stats *CollectionStats,
) float64 {
	// TODO: Implement histogram-based estimation
	// This would provide more accurate estimates for range predicates

	fieldStats := stats.FieldStats[fieldName]
	if fieldStats == nil {
		return e.config.DefaultSelectivity
	}

	return e.estimatePredicate(predicate, fieldStats)
}

// validateSelectivity ensures selectivity is in valid range [0, 1]
func validateSelectivity(selectivity float64) float64 {
	if selectivity < 0.0 {
		return 0.0
	}
	if selectivity > 1.0 {
		return 1.0
	}
	if typeutil.IsNaN(selectivity) {
		return 0.5 // Default fallback
	}
	return selectivity
}
