# RFC-0008: Comprehensive Index Health Metrics

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Observability & Monitoring  
**Priority:** Medium  
**Complexity:** Low-Medium (1-2 weeks)  
**POC Status:** Designed, straightforward implementation

## Summary

Add comprehensive Prometheus metrics for index lifecycle tracking: build success/failure rates, load times, query performance per index type, and health alerts. Current implementation lacks visibility into index-related issues, making diagnosis difficult and preventing proactive monitoring.

**Expected Impact:**
- Real-time health visibility for all indexes
- Automated alerting on anomalies
- Historical trend analysis (performance degradation detection)
- Better capacity planning data

## Motivation

### Problem Statement

**Limited visibility:**
- No tracking of build success/failure rates
- No load time metrics per index type
- No query performance broken down by index
- Hard to diagnose index-related issues

**Real-World Pain:**
- Index builds fail silently
- Slow loading goes unnoticed until queries timeout
- Can't compare HNSW vs IVF performance
- Reactive troubleshooting only

### Use Cases

**Use Case 1: Production Monitoring**
- Monitor index build success rate (should be >95%)
- Alert when build failures spike
- Track degradation over time

**Use Case 2: Performance Analysis**
- Compare query latency across index types
- Identify which indexes are slowest
- Guide optimization efforts

**Use Case 3: Capacity Planning**
- Track build times to predict scaling needs
- Monitor memory usage per index type
- Plan hardware upgrades

## Detailed Design

### Metrics Definition

**Location:** `internal/datanode/index/metrics.go` (enhanced)

```go
package index

import (
    "github.com/prometheus/client_golang/prometheus"
)

var (
    // Build metrics
    IndexBuildDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "milvus_index_build_duration_seconds",
            Help: "Index build duration by type and collection",
            Buckets: prometheus.ExponentialBuckets(1, 2, 12), // 1s to ~1h
        },
        []string{"index_type", "collection_id"},
    )
    
    IndexBuildSuccess = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "milvus_index_build_success_total",
            Help: "Successful index builds",
        },
        []string{"index_type"},
    )
    
    IndexBuildFailure = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "milvus_index_build_failure_total",
            Help: "Failed index builds",
        },
        []string{"index_type", "error_type"},
    )
    
    // Load metrics (QueryNode)
    IndexLoadDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "milvus_index_load_duration_seconds",
            Help: "Index load duration by type and size",
            Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
        },
        []string{"index_type", "segment_size_mb"},
    )
    
    // Search performance by index
    IndexSearchLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "milvus_index_search_latency_ms",
            Help: "Search latency by index type",
            Buckets: prometheus.ExponentialBuckets(1, 2, 12),
        },
        []string{"index_type", "collection_id"},
    )
    
    // Index memory usage
    IndexMemoryBytes = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "milvus_index_memory_bytes",
            Help: "Memory used by index",
        },
        []string{"index_type", "segment_id"},
    )
)
```

### Grafana Dashboard

```yaml
panels:
  - title: "Index Build Success Rate"
    query: |
      sum(rate(milvus_index_build_success_total[5m])) by (index_type) /
      (sum(rate(milvus_index_build_success_total[5m])) by (index_type) +
       sum(rate(milvus_index_build_failure_total[5m])) by (index_type))
    visualization: "gauge"
    thresholds:
      - value: 0.95
        color: "green"
      - value: 0.90
        color: "yellow"
      - value: 0
        color: "red"
  
  - title: "Index Build Duration by Type"
    query: |
      histogram_quantile(0.95,
        sum(rate(milvus_index_build_duration_seconds_bucket[10m])) by (le, index_type)
      )
    visualization: "bar_chart"
  
  - title: "Search Latency by Index Type"
    query: |
      histogram_quantile(0.95,
        sum(rate(milvus_index_search_latency_ms_bucket[5m])) by (le, index_type)
      )
    visualization: "time_series"
```

### Alerting Rules

```yaml
# Alert on high build failure rate
- alert: HighIndexBuildFailureRate
  expr: |
    sum(rate(milvus_index_build_failure_total[10m])) by (index_type) /
    sum(rate(milvus_index_build_total[10m])) by (index_type) > 0.1
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High index build failure rate for {{ $labels.index_type }}"
    description: "{{ $value | humanizePercentage }} build failure rate"

# Alert on slow index loading
- alert: SlowIndexLoading
  expr: |
    histogram_quantile(0.95,
      sum(rate(milvus_index_load_duration_seconds_bucket[10m])) by (le, index_type)
    ) > 60
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Slow index loading for {{ $labels.index_type }}"
    description: "p95 load time: {{ $value }} seconds"
```

## Expected Impact

- **Real-time visibility** into index health
- **Automated alerts** for anomalies
- **Historical trends** for capacity planning
- **Faster debugging** (know which index type is problematic)

## Drawbacks

1. **Metric Cardinality** - many labels could create many time series
2. **Storage Costs** - more metrics to store in Prometheus

## References

- Blog Post: [`blog/posts/06_next_gen_improvements.md:1060`](blog/posts/06_next_gen_improvements.md:1060)

---

**Status:** Ready for implementation - straightforward metrics addition