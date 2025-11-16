# Adaptive Query Routing

This package implements intelligent query routing for Milvus QueryCoord based on real-time node metrics.

## Overview

The Adaptive Query Routing system makes node selection decisions based on:
- **CPU Load**: Prefers nodes with lower CPU utilization
- **Memory Usage**: Prefers nodes with more available memory
- **Cache Locality**: Routes queries to nodes that have the required segments cached
- **Historical Performance**: Considers P95/P99 latency metrics

## Architecture

### Components

1. **AdaptiveRouter**: Main routing component that scores and selects nodes
2. **WeightedBalancer**: Selects top-N nodes based on calculated scores
3. **NodeMetrics**: Stores real-time metrics for each QueryNode
4. **RouterConfig**: Configuration parameters for routing behavior

### Scoring Algorithm

Each node receives a score based on weighted factors:

```
score = (1 - cpu_usage) * cpu_weight
      + (1 - memory_usage) * memory_weight
      + cache_locality * cache_weight
      + latency_score * latency_weight
```

Default weights:
- CPU: 0.3
- Memory: 0.2
- Cache: 0.3
- Latency: 0.2

## Configuration

Enable adaptive routing in `milvus.yaml`:

```yaml
queryCoord:
  adaptiveRouting:
    enabled: true
    cpuWeight: 0.3
    memoryWeight: 0.2
    cacheWeight: 0.3
    latencyWeight: 0.2
    maxCPUUsage: 0.9
    maxMemoryUsage: 0.85
    minHealthScore: 0.3
    metricsUpdateInterval: 5
    rebalanceInterval: 30
```

## Usage

### Creating a Router

```go
import "github.com/milvus-io/milvus/internal/querycoordv2/routing"

// Use default configuration
config := routing.DefaultRouterConfig()
router := routing.NewAdaptiveRouter(config)

// Or load from paramtable
config := routing.LoadConfigFromParams()
router := routing.NewAdaptiveRouter(config)
```

### Updating Node Metrics

```go
metrics := &routing.NodeMetrics{
    NodeID:        1,
    CPUUsage:      0.45,
    MemoryUsage:   0.60,
    CacheHitRate:  0.75,
    LatencyP95:    25.0,
    LatencyP99:    40.0,
    QPS:           1000,
    ActiveQueries: 15,
    LocalSegments: map[int64]bool{
        100: true,
        101: true,
    },
}

router.UpdateNodeMetrics(1, metrics)
```

### Routing a Query

```go
import "context"

ctx := context.Background()
req := &querypb.SearchRequest{
    Req: &querypb.SearchRequest_InternalSearchRequest{
        InternalSearchRequest: &querypb.InternalSearchRequest{
            SegmentIDs: []int64{100, 101},
            Nq:         10,
        },
    },
}

selectedNodes, err := router.RouteQuery(ctx, req)
if err != nil {
    // Handle error
}
// Use selectedNodes for query execution
```

## Health Checks

Nodes are excluded from routing if they meet any of these conditions:
- CPU usage > `maxCPUUsage` (default: 0.9)
- Memory usage > `maxMemoryUsage` (default: 0.85)
- Health score < `minHealthScore` (default: 0.3)
- Metrics are stale (older than 30 seconds)

## Integration Points

To integrate adaptive routing with existing QueryCoord:

1. **Create router instance** during QueryCoord initialization
2. **Update metrics periodically** from QueryNode heartbeats
3. **Call RouteQuery()** when dispatching search requests
4. **Remove nodes** when they become unavailable

Example integration:

```go
// In QueryCoord initialization
if routing.IsAdaptiveRoutingEnabled() {
    qc.adaptiveRouter = routing.NewAdaptiveRouter(routing.LoadConfigFromParams())
}

// In heartbeat handler
if qc.adaptiveRouter != nil {
    metrics := extractMetricsFromHeartbeat(heartbeat)
    qc.adaptiveRouter.UpdateNodeMetrics(nodeID, metrics)
}

// In query dispatcher
if qc.adaptiveRouter != nil {
    nodes, err := qc.adaptiveRouter.RouteQuery(ctx, req)
    // Use selected nodes
} else {
    // Fall back to existing routing logic
}
```

## Testing

Run tests:

```bash
go test ./internal/querycoordv2/routing/...
```

Run with coverage:

```bash
go test ./internal/querycoordv2/routing/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Performance Considerations

- **Memory overhead**: ~1KB per node for metrics storage
- **CPU overhead**: ~0.5% for metrics updates and scoring
- **Latency**: Routing decision adds <1ms to query path

## Expected Benefits

Based on RFC-0001:
- **15-30% latency reduction** for queries hitting hot segments
- **20% improvement** in cache hit rates
- **More balanced load** distribution (variance <0.15)

## Future Enhancements

1. **Auto-tuning weights** based on workload patterns
2. **Circuit breaker** integration for overloaded nodes
3. **Weighted random selection** for better distribution
4. **Multi-objective optimization** using machine learning
5. **Tenant-aware routing** for multi-tenant deployments

## References

- RFC-0001: Adaptive Query Routing for QueryCoord
- [QueryCoord Scheduler](../../task/scheduler.go)
- [Balance Interface](../../balance/balance.go)
