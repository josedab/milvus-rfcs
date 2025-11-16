# Self-Optimizing Index Parameters

This package implements automatic parameter optimization for Milvus index parameters based on observed query metrics, data distribution changes, and SLA targets.

## Overview

The self-optimizing index parameter system continuously monitors query performance and automatically adjusts index parameters (HNSW `ef`, IVF `nprobe`, etc.) to maintain optimal cost/performance balance.

**Key Benefits:**
- 20-40% cost savings from automated right-sizing
- Continuous improvement as data and query patterns evolve
- Reduced operational burden (no manual tuning required)
- Adaptive to changes in data distribution

## Architecture

The system consists of four main components:

### 1. MetricsCollector

Collects and aggregates query performance metrics over time.

**Collected Metrics:**
- Query latency (P50, P95, P99)
- Recall rates
- Memory usage
- CPU usage
- Current parameter values

**Configuration:**
- Retention period: How long to keep historical metrics (default: 7 days)
- Max samples: Maximum number of samples per collection (default: 10,000)

### 2. PerformanceAnalyzer

Analyzes collected metrics to detect performance issues.

**Detected Issues:**
- `high_latency`: P95 latency exceeds target
- `low_recall`: Mean recall below target
- `high_memory`: Memory usage approaching budget limit
- `over_provisioned`: System exceeds targets (opportunity to save costs)

**Performance Targets:**
```go
type PerformanceTarget struct {
    TargetLatency    time.Duration  // P95 latency target (default: 50ms)
    LatencyTolerance float64        // Acceptable overage (default: 1.2 = 20%)
    TargetRecall     float64        // Recall target (default: 0.95)
    RecallTolerance  float64        // Minimum acceptable (default: 0.95)
    MemoryBudget     int64          // Memory limit in bytes (default: 10GB)
    MemoryTolerance  float64        // Threshold to optimize (default: 0.9 = 90%)
}
```

### 3. DecisionEngine

Generates optimization suggestions based on performance analysis.

**Optimization Actions:**

| Issue | Index Type | Action | Impact |
|-------|-----------|--------|--------|
| Low Recall | HNSW | Increase `ef` by 50% | +5% recall, +30% latency |
| Low Recall | IVF | Increase `nprobe` by 50% | +5% recall, +25% latency |
| High Latency | HNSW | Decrease `ef` by 20% | -20% latency, -2% recall |
| High Latency | IVF | Decrease `nprobe` by 25% | -25% latency, -3% recall |
| High Memory | HNSW | Rebuild with smaller `M` | -25% memory, requires reindex |
| Over-provisioned | HNSW | Decrease `ef` by 25% | -15% latency, cost savings |

### 4. AutoTuner

Main orchestrator that integrates all components and implements the Checker interface.

**Execution Flow:**
1. Every 24 hours, run optimization check
2. Clean up old metrics
3. For each collection:
   - Get aggregated metrics from last 24 hours
   - Analyze performance vs. targets
   - Generate optimization suggestion if needed
4. Log suggestions (Phase 1: Monitoring only)

## Implementation Phases

### Phase 1: Monitoring (CURRENT)

**Status:** ✅ Implemented

**Capabilities:**
- Collect query metrics
- Analyze performance
- Generate optimization suggestions
- Log suggestions for review

**Limitations:**
- Does not automatically apply changes
- No integration with query execution path
- Manual review required

### Phase 2: Decision Logic (FUTURE)

**Planned:**
- Enhanced decision algorithms
- Bayesian optimization for parameter search
- Multi-objective optimization (latency + recall + cost)
- Historical data analysis

### Phase 3: Safe Parameter Updates (FUTURE)

**Planned:**
- Automatic application of suggestions
- Gradual rollout with canary testing
- Rollback on degradation
- Integration with QueryCoord job system

### Phase 4: Bayesian Optimization (FUTURE)

**Planned:**
- Gaussian Process-based parameter search
- Predictive modeling of parameter impact
- Exploration vs. exploitation balancing

## Usage

### Basic Usage

```go
// Create auto tuner
tuner := NewAutoTuner()

// Set custom performance targets
target := PerformanceTarget{
    TargetLatency:    30 * time.Millisecond,
    LatencyTolerance: 1.2,
    TargetRecall:     0.98,
    RecallTolerance:  0.95,
    MemoryBudget:     20 * 1024 * 1024 * 1024, // 20GB
    MemoryTolerance:  0.9,
}
tuner.SetPerformanceTarget(collectionID, target)

// Activate tuner
tuner.Activate()

// Record query metrics (integrate with query execution)
metrics := QueryMetrics{
    CollectionID: 1,
    IndexType:    "HNSW",
    Latency:      45 * time.Millisecond,
    Recall:       0.96,
    MemoryUsage:  1024 * 1024 * 1024,
    CPUUsage:     0.5,
    SearchParams: map[string]interface{}{"ef": 64},
    IndexParams:  map[string]interface{}{"M": 16},
}
tuner.RecordQueryMetrics(metrics)

// Get current metrics
currentMetrics := tuner.GetMetrics(collectionID)

// Get performance analysis
analysis := tuner.GetAnalysis(collectionID)

// Get optimization suggestion
suggestion := tuner.GetSuggestion(collectionID)
if suggestion != nil {
    log.Printf("Suggestion: %s - %s", suggestion.Action, suggestion.Reason)
}
```

### Integration with QueryCoordV2

The `AutoTuner` implements the `Checker` interface and can be integrated into QueryCoordV2's checker system:

```go
// In server.go
func (s *Server) initCheckers() {
    // ... existing checkers ...

    // Add auto tuner
    s.autoTuner = optimization.NewAutoTuner()
    s.checkers = append(s.checkers, s.autoTuner)
}

// In query execution path
func (s *Server) executeQuery(req *SearchRequest) (*SearchResult, error) {
    start := time.Now()
    result, err := s.doSearch(req)
    latency := time.Since(start)

    // Record metrics for optimization
    if s.autoTuner.IsActive() {
        metrics := optimization.QueryMetrics{
            CollectionID: req.CollectionID,
            IndexType:    getIndexType(req.CollectionID),
            Latency:      latency,
            Recall:       computeRecall(result),
            MemoryUsage:  getCurrentMemory(),
            CPUUsage:     getCurrentCPU(),
            SearchParams: req.SearchParams,
        }
        s.autoTuner.RecordQueryMetrics(metrics)
    }

    return result, err
}
```

## Prometheus Metrics

The system exports the following Prometheus metrics:

### Counters

- `milvus_querycoord_optimization_suggestions_total{collection_id, action, issue_type}`
  - Total optimization suggestions generated

- `milvus_querycoord_optimization_actions_total{collection_id, action, result}`
  - Total optimization actions applied

### Histograms

- `milvus_querycoord_optimization_latency_improvement{collection_id, action}`
  - Latency improvement percentage after optimization

- `milvus_querycoord_optimization_recall_change{collection_id, action}`
  - Recall change percentage after optimization

### Gauges

- `milvus_querycoord_optimization_p95_latency_ms{collection_id, index_type}`
  - Current P95 query latency

- `milvus_querycoord_optimization_mean_recall{collection_id, index_type}`
  - Current mean recall

- `milvus_querycoord_optimization_memory_usage_bytes{collection_id, index_type}`
  - Current memory usage

- `milvus_querycoord_optimization_parameter_value{collection_id, parameter_name}`
  - Current parameter values (ef, nprobe, M)

- `milvus_querycoord_optimization_enabled`
  - Whether auto-tuning is enabled

## Configuration

Configuration is managed through `paramtable`:

```go
// Enable/disable auto-tuning
params.QueryCoordCfg.EnableAutoTuning = true

// Check interval (how often to analyze)
params.QueryCoordCfg.OptimizationCheckInterval = 24 * time.Hour

// Metrics window (time range to analyze)
params.QueryCoordCfg.OptimizationMetricsWindow = 24 * time.Hour

// Minimum samples required for decisions
params.QueryCoordCfg.OptimizationMinSamples = 100
```

## Testing

Run tests:

```bash
go test ./internal/querycoordv2/optimization/...
```

Run with coverage:

```bash
go test -cover ./internal/querycoordv2/optimization/...
```

## Future Enhancements

1. **Automatic Application (Phase 3)**
   - Implement `ParameterUpdateTask` and `ParameterUpdateJob`
   - Add rollback mechanism
   - Gradual rollout with monitoring

2. **Bayesian Optimization (Phase 4)**
   - Implement Gaussian Process models
   - Multi-objective optimization
   - Exploration vs. exploitation

3. **Advanced Metrics**
   - Cost tracking (CPU time, memory cost)
   - User satisfaction metrics
   - Query complexity analysis

4. **ML-based Prediction**
   - Predict parameter impact before applying
   - Learn from historical changes
   - Anomaly detection

## References

- RFC: [`rfcs/0014-self-optimizing-index-parameters.md`](../../../rfcs/0014-self-optimizing-index-parameters.md)
- Blog Post: [`blog/posts/06_next_gen_improvements.md`](../../../blog/posts/06_next_gen_improvements.md)
