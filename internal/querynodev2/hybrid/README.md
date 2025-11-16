# Hybrid Search Optimizer

This package implements the optimized hybrid search execution framework as described in **RFC-0004: Optimized Hybrid Search Execution**.

## Overview

The hybrid search optimizer dynamically selects the optimal execution plan for hybrid search (vector + scalar filter) based on filter selectivity estimation. This provides 2-10x speedup for filtered queries depending on selectivity.

## Architecture

### Components

1. **HybridSearchOptimizer** (`optimizer.go`)
   - Main orchestrator that selects execution plans
   - Estimates filter selectivity using statistics
   - Routes to appropriate execution plan based on selectivity thresholds

2. **SelectivityEstimator** (`selectivity_estimator.go`)
   - Estimates what fraction of data passes the filter
   - Uses collection statistics and filter expression analysis
   - Supports equality, range, IN/NOT IN, and compound predicates

3. **StatisticsCache** (`statistics_cache.go`)
   - Thread-safe cache for collection statistics
   - Stores field cardinality, value distributions, and min/max values
   - Supports automatic cache eviction

4. **Execution Plans** (`execution_plans.go`)
   - `FilterThenSearchPlan`: Filter first, then search (<1% selectivity)
   - `SearchThenFilterPlan`: Search first, then filter (>50% selectivity)
   - `ParallelHybridPlan`: Parallel execution (1-50% selectivity)

## Usage

### Creating an Optimizer

```go
import "github.com/milvus-io/milvus/internal/querynodev2/hybrid"

// Create optimizer with default configuration
optimizer := hybrid.NewHybridSearchOptimizer(segmentManager, nil)

// Or with custom configuration
config := &hybrid.OptimizerConfig{
    HighlySelectiveThreshold: 0.01,  // 1%
    BroadFilterThreshold:     0.50,  // 50%
    EnableParallelExecution:  true,
    DefaultSelectivity:       0.50,
}
optimizer := hybrid.NewHybridSearchOptimizer(segmentManager, config)
```

### Updating Statistics

```go
// Create collection statistics
stats := &hybrid.CollectionStats{
    TotalRows: 1000000,
    FieldStats: map[string]*hybrid.FieldStats{
        "category": {
            Cardinality: 100,
            TotalCount:  1000000,
            Distribution: map[string]int64{
                "electronics": 500000,
                "home":        300000,
                "sports":      200000,
            },
        },
    },
}

// Update optimizer's cache
optimizer.UpdateStatistics(collectionID, stats)
```

### Selecting an Execution Plan

```go
// Create hybrid search request
req := &hybrid.HybridSearchRequest{
    SearchRequest:    internalSearchReq,
    SealedSegments:   sealedSegs,
    GrowingSegments:  growingSegs,
}

// Let optimizer select the best plan
plan := optimizer.OptimizePlan(ctx, req)

// Check which plan was selected
switch plan.PlanType() {
case hybrid.PlanFilterThenSearch:
    // Highly selective filter - filter first
case hybrid.PlanSearchThenFilter:
    // Broad filter - search first
case hybrid.PlanParallelHybrid:
    // Moderate selectivity - parallel execution
}
```

## Plan Selection Logic

The optimizer uses the following thresholds:

- **Selectivity < 1%**: FilterThenSearchPlan
  - Filter is highly selective
  - Best to filter data first to minimize search space
  - Example: `id = 123` or `user_id = 'specific-user'`

- **Selectivity > 50%**: SearchThenFilterPlan
  - Filter is broad (matches >50% of data)
  - Better to search all data and filter small result set
  - Example: `category IN ['electronics', 'home', 'sports']` (90% of data)

- **Selectivity 1-50%**: ParallelHybridPlan (if enabled)
  - Moderate selectivity
  - Parallel execution of filter and search
  - Example: `rating >= 4.0` (30% of data)

## Selectivity Estimation

### Expression Complexity Heuristic

For simple estimation without full expression parsing:

- Expression size < 50 bytes: 0.1% selectivity
- Expression size < 100 bytes: 1% selectivity
- Expression size < 200 bytes: 10% selectivity
- Expression size < 400 bytes: 30% selectivity
- Expression size >= 400 bytes: 60% selectivity

### Predicate-Based Estimation

For more accurate estimation (future enhancement):

- **Equality (`=`)**: `1 / cardinality`
- **Range (`>`, `<`)**: Uses histograms or defaults to 33-50%
- **IN clause**: Sum of individual value frequencies
- **NOT IN**: `1 - (IN selectivity)`
- **AND**: Multiply selectivities (intersection)
- **OR**: `1 - ∏(1 - sᵢ)` (union)

## Performance Expectations

Based on RFC benchmarks:

| Filter Selectivity | Baseline (ms) | Optimized (ms) | Speedup |
|-------------------|---------------|----------------|---------|
| 1% (selective)    | 5             | 5              | 1.0x    |
| 10%               | 15            | 8              | 1.9x    |
| 30%               | 45            | 15             | 3.0x    |
| 50%               | 75            | 12             | 6.3x    |
| 80% (broad)       | 120           | 11             | 10.9x   |

## Integration Notes

This implementation provides the core optimizer logic and plan selection framework. Full integration with Milvus requires:

1. **Delegator Integration**: Modify `internal/querynodev2/delegator/delegator.go` to use the optimizer
2. **Statistics Collection**: Implement background statistics gathering from segments
3. **Search Request Creation**: Properly construct `*segcore.SearchRequest` with collection metadata
4. **Plan Execution**: Integrate execution plans with existing search infrastructure

## Testing

Run tests with:

```bash
go test ./internal/querynodev2/hybrid/...
```

Test coverage:
- Optimizer plan selection logic
- Selectivity estimation algorithms
- Statistics cache operations
- Execution plan cost models

## References

- RFC: `rfcs/0004-optimized-hybrid-search.md`
- Blog Post: `blog/posts/06_next_gen_improvements.md:753`
- Existing Search: `internal/querynodev2/delegator/delegator.go`

## Future Enhancements

1. **Full Expression Parsing**: Parse serialized expression plan for accurate predicate analysis
2. **Histogram Support**: Use field histograms for precise range query estimation
3. **Adaptive Thresholds**: Dynamically adjust thresholds based on query performance
4. **Statistics Auto-Collection**: Automatically gather and update field statistics
5. **Cost Model Refinement**: Calibrate cost models based on actual query latencies
