# Index Health Metrics (RFC-0008)

This document describes the comprehensive index health metrics implemented as part of RFC-0008.

## Overview

The index health metrics provide real-time visibility into the index lifecycle in Milvus, including:
- Build success/failure rates
- Build duration by index type
- Load times by index type and segment size
- Query performance per index type
- Memory usage by index type

## Metrics Reference

### DataNode/IndexNode Metrics

#### `milvus_indexnode_index_build_duration_seconds`
- **Type:** Histogram
- **Labels:** `index_type`, `collection_id`
- **Unit:** Seconds
- **Description:** Measures the total time to build an index from start to completion
- **Buckets:** Exponential from 1s to ~1h (1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096)
- **Use Cases:**
  - Track build performance trends
  - Identify slow index types
  - Capacity planning for index builds
  - SLA monitoring

**Example Queries:**
```promql
# p95 build duration by index type
histogram_quantile(0.95,
  sum(rate(milvus_indexnode_index_build_duration_seconds_bucket[10m])) by (le, index_type)
)

# Average build duration per collection
avg(rate(milvus_indexnode_index_build_duration_seconds_sum[5m])) by (collection_id)
```

#### `milvus_indexnode_index_build_success_total`
- **Type:** Counter
- **Labels:** `index_type`
- **Description:** Total number of successful index builds
- **Use Cases:**
  - Calculate success rates
  - Monitor build throughput
  - Track activity by index type

**Example Queries:**
```promql
# Build success rate by index type
sum(rate(milvus_indexnode_index_build_success_total[5m])) by (index_type) /
(sum(rate(milvus_indexnode_index_build_success_total[5m])) by (index_type) +
 sum(rate(milvus_indexnode_index_build_failure_total[5m])) by (index_type))

# Total successful builds in last hour
increase(milvus_indexnode_index_build_success_total[1h])
```

#### `milvus_indexnode_index_build_failure_total`
- **Type:** Counter
- **Labels:** `index_type`, `error_type`
- **Description:** Total number of failed index builds with error classification
- **Use Cases:**
  - Identify failure patterns
  - Root cause analysis
  - Alert on high failure rates
  - Track specific error types

**Example Queries:**
```promql
# Failure rate by error type
sum(rate(milvus_indexnode_index_build_failure_total[5m])) by (error_type)

# Most common failure reasons
topk(5, sum(increase(milvus_indexnode_index_build_failure_total[1h])) by (error_type))
```

#### `milvus_indexnode_index_memory_bytes`
- **Type:** Gauge
- **Labels:** `index_type`, `segment_id`
- **Description:** Current memory usage by loaded indexes
- **Unit:** Bytes
- **Use Cases:**
  - Monitor memory consumption
  - Capacity planning
  - Identify memory leaks
  - Track per-segment memory usage

**Example Queries:**
```promql
# Total memory by index type
sum(milvus_indexnode_index_memory_bytes) by (index_type)

# Top 10 segments by memory usage
topk(10, milvus_indexnode_index_memory_bytes)

# Memory growth rate
rate(milvus_indexnode_index_memory_bytes[5m])
```

### QueryNode Metrics

#### `milvus_querynode_index_load_duration_seconds`
- **Type:** Histogram
- **Labels:** `index_type`, `segment_size_mb`
- **Unit:** Seconds
- **Description:** Time to load an index into memory on QueryNode
- **Buckets:** Exponential from 0.1s to ~100s (0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 25.6, 51.2)
- **Use Cases:**
  - Monitor load performance
  - Correlate load time with segment size
  - Optimize cold start times
  - Identify I/O bottlenecks

**Example Queries:**
```promql
# p95 load duration by index type
histogram_quantile(0.95,
  sum(rate(milvus_querynode_index_load_duration_seconds_bucket[10m])) by (le, index_type)
)

# Load duration correlation with segment size
avg(rate(milvus_querynode_index_load_duration_seconds_sum[5m])) by (segment_size_mb)
```

#### `milvus_querynode_index_search_latency_ms`
- **Type:** Histogram
- **Labels:** `index_type`, `collection_id`
- **Unit:** Milliseconds
- **Description:** Search latency broken down by index type
- **Buckets:** Exponential from 1ms to ~4s (1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096)
- **Use Cases:**
  - Compare performance across index types (HNSW vs IVF vs FLAT)
  - Identify performance regressions
  - Optimize query performance
  - SLA monitoring per index type

**Example Queries:**
```promql
# p99 search latency by index type
histogram_quantile(0.99,
  sum(rate(milvus_querynode_index_search_latency_ms_bucket[5m])) by (le, index_type)
)

# Compare HNSW vs IVF performance
histogram_quantile(0.95,
  sum(rate(milvus_querynode_index_search_latency_ms_bucket{index_type=~"HNSW|IVF_FLAT"}[5m])) by (le, index_type)
)
```

## Common Use Cases

### 1. Monitor Index Build Health

**Goal:** Ensure >95% build success rate

```promql
# Alert if success rate drops below 95%
sum(rate(milvus_indexnode_index_build_success_total[5m])) by (index_type) /
(sum(rate(milvus_indexnode_index_build_success_total[5m])) by (index_type) +
 sum(rate(milvus_indexnode_index_build_failure_total[5m])) by (index_type)) < 0.95
```

### 2. Compare Index Type Performance

**Goal:** Determine which index type performs best for your workload

```promql
# Build duration comparison
histogram_quantile(0.95,
  sum(rate(milvus_indexnode_index_build_duration_seconds_bucket[1h])) by (le, index_type)
)

# Search latency comparison
histogram_quantile(0.95,
  sum(rate(milvus_querynode_index_search_latency_ms_bucket[1h])) by (le, index_type)
)
```

### 3. Capacity Planning

**Goal:** Predict when to scale based on build times and memory usage

```promql
# Memory usage trend
deriv(avg_over_time(sum(milvus_indexnode_index_memory_bytes)[1h:5m])[1d:1h])

# Build time trend
avg_over_time(
  histogram_quantile(0.95,
    sum(rate(milvus_indexnode_index_build_duration_seconds_bucket[1h])) by (le, index_type)
  )[1d:1h]
)
```

### 4. Diagnose Performance Degradation

**Goal:** Identify when and why performance is degrading

```promql
# Compare current vs 24h ago
(
  histogram_quantile(0.95, sum(rate(milvus_querynode_index_search_latency_ms_bucket[1h])) by (le, index_type))
  -
  histogram_quantile(0.95, sum(rate(milvus_querynode_index_search_latency_ms_bucket[1h] offset 24h)) by (le, index_type))
) /
histogram_quantile(0.95, sum(rate(milvus_querynode_index_search_latency_ms_bucket[1h] offset 24h)) by (le, index_type))
```

## Dashboard Setup

### Installing the Grafana Dashboard

1. Navigate to `deployments/monitor/grafana/`
2. Import `index-health-dashboard.json` into your Grafana instance
3. Configure the Prometheus datasource

The dashboard includes:
- **Index Build Success Rate** - Gauge showing success rate by index type
- **Index Build Duration** - Bar chart of p95 build times
- **Build Failures** - Time series of failures by error type
- **Search Latency** - Time series comparing p95/p99 latency
- **Load Duration** - Time series of index load times
- **Memory Usage** - Stacked area chart of memory by index type
- **Build Rate** - Stacked area chart of success/failure rates
- **Load Duration by Size** - Heatmap correlating size and load time

## Alerting Setup

### Installing Prometheus Alerts

1. Navigate to `deployments/monitor/prometheus/`
2. Add `index-health-alerts.yml` to your Prometheus configuration:

```yaml
# prometheus.yml
rule_files:
  - /path/to/index-health-alerts.yml
```

3. Reload Prometheus configuration

### Alert Severity Levels

- **Critical:** Immediate action required (>25% failure rate, >5s search latency, >5min load time)
- **Warning:** Investigation recommended (>10% failure rate, >1s search latency, >1min load time)
- **Info:** Informational alerts for trends (latency increasing over time)

## Best Practices

### 1. Label Cardinality
- `index_type`: Low cardinality (HNSW, IVF_FLAT, IVF_SQ8, FLAT, etc.) - ~10 values
- `collection_id`: Medium cardinality - depends on your workload
- `segment_id`: High cardinality - use sparingly in queries, aggregate when possible
- `error_type`: Low-medium cardinality - categorized error types

**Recommendation:** Use aggregation functions (`sum`, `avg`) to reduce cardinality in queries.

### 2. Recording Rules
For frequently-used queries, create recording rules:

```yaml
# prometheus-rules.yml
groups:
  - name: index_health_recording
    interval: 30s
    rules:
      - record: milvus:index_build_success_rate:5m
        expr: |
          sum(rate(milvus_indexnode_index_build_success_total[5m])) by (index_type) /
          (sum(rate(milvus_indexnode_index_build_success_total[5m])) by (index_type) +
           sum(rate(milvus_indexnode_index_build_failure_total[5m])) by (index_type))

      - record: milvus:index_search_latency_p95:5m
        expr: |
          histogram_quantile(0.95,
            sum(rate(milvus_querynode_index_search_latency_ms_bucket[5m])) by (le, index_type)
          )
```

### 3. Retention and Storage
- **High-resolution data:** 15 days
- **Downsampled data:** 90 days
- **Long-term trends:** 1 year (using recording rules)

### 4. Query Performance
- Use `rate()` for counters, not `increase()` when calculating rates
- Prefer `histogram_quantile()` over `avg()` for latency metrics
- Use appropriate time ranges (5m for real-time, 1h for trends)
- Leverage recording rules for complex queries used in dashboards

## Troubleshooting

### High Build Failure Rate

**Symptoms:** `HighIndexBuildFailureRate` alert firing

**Investigation Steps:**
1. Check error types: `topk(5, sum(rate(milvus_indexnode_index_build_failure_total[5m])) by (error_type))`
2. Review IndexNode logs for detailed errors
3. Check resource availability (CPU, memory, disk)
4. Verify data quality and schema compatibility

### Slow Index Loading

**Symptoms:** `SlowIndexLoading` alert firing

**Investigation Steps:**
1. Check segment sizes: `histogram_quantile(0.95, sum(rate(milvus_querynode_index_load_duration_seconds_bucket[10m])) by (le, segment_size_mb))`
2. Monitor disk I/O on QueryNodes
3. Check network bandwidth if using remote storage
4. Verify memory availability

### Degraded Search Performance

**Symptoms:** `DegradedIndexSearchPerformance` alert firing

**Investigation Steps:**
1. Compare with baseline: See "Diagnose Performance Degradation" query above
2. Check CPU utilization on QueryNodes
3. Review index quality and configuration
4. Analyze query patterns for optimization opportunities
5. Verify no resource contention

## Future Enhancements

Potential additions to these metrics (not in RFC-0008):

1. **Index Quality Metrics**
   - Recall rate per index type
   - Index size vs raw data size ratio

2. **Resource Metrics**
   - CPU usage during build/search
   - Disk I/O during load operations

3. **Cost Metrics**
   - Build cost (CPU-seconds, memory-seconds)
   - Storage cost per index type

## References

- [RFC-0008: Comprehensive Index Health Metrics](../../rfcs/0008-index-health-metrics.md)
- [Milvus Index Documentation](https://milvus.io/docs/index.md)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/naming/)
- [Grafana Dashboard Design](https://grafana.com/docs/grafana/latest/dashboards/)
