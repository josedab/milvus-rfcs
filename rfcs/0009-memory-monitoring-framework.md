# RFC-0009: Memory Monitoring Framework

**Status:** Implemented
**Author:** Jose David Baena
**Created:** 2025-04-03
**Implemented:** 2025-11-16
**Category:** Observability & Monitoring
**Priority:** Medium
**Complexity:** Medium (2-3 weeks)
**POC Status:** Fully implemented

## Summary

Implement comprehensive memory monitoring and alerting framework that tracks memory usage per component, index type, and segment. Provides early warning for OOM conditions, memory leaks, and over-provisioning. Current lack of granular memory visibility makes capacity planning difficult and OOM incidents hard to diagnose.

**Expected Impact:**
- Prevent OOM incidents through early warning
- Better capacity planning (right-size resources)
- Identify memory leaks proactively
- Optimize memory allocation per index type

## Motivation

### Problem Statement

**Current limitations:**
- Only total memory usage tracked
- No per-component breakdown
- No memory leak detection
- OOM happens unexpectedly

**Real incidents:**
- QueryNode OOM after loading 50 HNSW segments (no warning)
- Memory leak in DataNode goes unnoticed for weeks
- Can't determine which index type uses most memory

### Use Cases

**Use Case 1: OOM Prevention**
- Monitor memory trends (90% → 95% → alert!)
- Proactive pod eviction before OOM
- **Impact: Zero downtime**

**Use Case 2: Capacity Planning**
- Track memory per index type (HNSW: 25GB, IVF: 18GB)
- Right-size QueryNode resources
- **Impact: 20% cost reduction**

**Use Case 3: Memory Leak Detection**
- Detect slowly growing memory
- Alert on anomalous growth rate
- **Impact: Faster bug detection**

## Detailed Design

### Memory Tracking

**Location:** `internal/util/hardware/memory_monitor.go` (new)

```go
package hardware

import (
    "runtime"
    "time"
    
    "github.com/prometheus/client_golang/prometheus"
)

type MemoryMonitor struct {
    // Metrics
    ComponentMemory *prometheus.GaugeVec
    IndexMemory     *prometheus.GaugeVec
    SegmentMemory   *prometheus.GaugeVec
    
    // Leak detection
    baselineMemory  uint64
    lastMeasurement time.Time
}

func NewMemoryMonitor() *MemoryMonitor {
    return &MemoryMonitor{
        ComponentMemory: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "milvus_component_memory_bytes",
                Help: "Memory usage by component",
            },
            []string{"component", "node_id"},
        ),
        IndexMemory: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "milvus_index_memory_bytes",
                Help: "Memory usage by index type",
            },
            []string{"index_type", "collection_id"},
        ),
    }
}

// MonitorLoop runs periodic memory checks
func (m *MemoryMonitor) MonitorLoop() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        m.collectMetrics()
        m.detectLeaks()
        m.checkThresholds()
    }
}

func (m *MemoryMonitor) collectMetrics() {
    var stats runtime.MemStats
    runtime.ReadMemStats(&stats)
    
    // Total memory
    m.ComponentMemory.WithLabelValues("total", getNodeID()).
        Set(float64(stats.Alloc))
    
    // Heap memory
    m.ComponentMemory.WithLabelValues("heap", getNodeID()).
        Set(float64(stats.HeapAlloc))
    
    // Stack memory
    m.ComponentMemory.WithLabelValues("stack", getNodeID()).
        Set(float64(stats.StackInuse))
}

func (m *MemoryMonitor) detectLeaks() {
    var stats runtime.MemStats
    runtime.ReadMemStats(&stats)
    
    current := stats.Alloc
    
    if m.baselineMemory > 0 {
        // Check growth rate
        timeSince := time.Since(m.lastMeasurement)
        growth := current - m.baselineMemory
        growthRate := float64(growth) / timeSince.Hours() // bytes per hour
        
        // Alert if growing >100MB/hour continuously
        if growthRate > 100*1024*1024 {
            log.Warn("Potential memory leak detected",
                zap.Float64("growth_rate_mb_per_hour", growthRate/1024/1024))
        }
    }
    
    m.baselineMemory = current
    m.lastMeasurement = time.Now()
}

func (m *MemoryMonitor) checkThresholds() {
    totalMemory := getTotalMemory()
    var stats runtime.MemStats
    runtime.ReadMemStats(&stats)
    
    usage := float64(stats.Alloc) / float64(totalMemory)
    
    if usage > 0.90 {
        log.Error("Critical memory usage",
            zap.Float64("usage_percent", usage*100))
    } else if usage > 0.80 {
        log.Warn("High memory usage",
            zap.Float64("usage_percent", usage*100))
    }
}
```

### Grafana Dashboard

```yaml
panels:
  - title: "Memory Usage by Component"
    query: |
      milvus_component_memory_bytes{component!="total"}
    visualization: "stacked_area"
  
  - title: "Memory Growth Rate"
    query: |
      rate(milvus_component_memory_bytes{component="total"}[5m]) * 3600
    visualization: "graph"
    unit: "bytes/hour"
  
  - title: "Memory Usage %"
    query: |
      (milvus_component_memory_bytes{component="total"} / 
       node_memory_MemTotal_bytes) * 100
    visualization: "gauge"
    thresholds: [80, 90, 95]
```

### Alerting Rules

```yaml
- alert: HighMemoryUsage
  expr: |
    (milvus_component_memory_bytes{component="total"} / 
     node_memory_MemTotal_bytes) > 0.85
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High memory usage on {{ $labels.node_id }}"
    description: "{{ $value }}% memory used"

- alert: MemoryLeak
  expr: |
    rate(milvus_component_memory_bytes{component="heap"}[1h]) > 10485760
  for: 2h
  labels:
    severity: critical
  annotations:
    summary: "Potential memory leak on {{ $labels.node_id }}"
    description: "Growing {{ $value }} bytes/sec"
```

## Expected Impact

- **Zero OOM incidents** through early warning
- **20% cost reduction** from right-sizing
- **Faster leak detection** (hours vs weeks)

## Drawbacks

1. **Monitoring Overhead** - Periodic metric collection (~1% CPU)
2. **False Positives** - Temporary spikes may trigger alerts

## References

- Go runtime metrics documentation
- Prometheus memory monitoring best practices

---

## Implementation Notes

**Implementation Date:** 2025-11-16

### Files Created

1. **Core Implementation**
   - `pkg/util/hardware/memory_monitor.go` - Main memory monitoring implementation
   - `pkg/util/hardware/memory_monitor_test.go` - Comprehensive unit tests
   - `pkg/metrics/memory_metrics.go` - Prometheus metrics definitions

2. **Monitoring Configuration**
   - `deployments/monitor/grafana/memory-monitoring-dashboard.json` - Grafana dashboard with 6 panels
   - `deployments/monitor/prometheus/memory-alerts.yml` - 7 Prometheus alert rules
   - `deployments/monitor/prometheus/README.md` - Alerting documentation

3. **Documentation**
   - `docs/memory-monitoring-integration.md` - Integration guide for developers

### Files Modified

- `pkg/metrics/metrics.go` - Added registration of memory metrics

### Features Implemented

✅ Component-level memory tracking (total, heap, stack, GC)
✅ Memory usage percentage calculation
✅ Memory leak detection with configurable threshold (100 MB/hour)
✅ Threshold-based alerting (warning at 80%, critical at 90%)
✅ Index memory tracking API
✅ Segment memory tracking API
✅ Automatic memory growth rate calculation
✅ Grafana dashboard with 6 visualization panels
✅ 7 Prometheus alert rules with different severity levels
✅ Comprehensive unit tests and benchmarks
✅ Integration documentation with examples

### Integration Points

The memory monitor can be integrated into any Milvus component:

```go
monitor := hardware.NewMemoryMonitor()
monitor.Start()
defer monitor.Stop()
```

Track index memory:
```go
hardware.RecordIndexMemory(indexType, collectionID, memoryBytes)
```

Track segment memory:
```go
hardware.RecordSegmentMemory(segmentID, collectionID, memoryBytes)
```

### Metrics Exposed

- `milvus_component_memory_bytes` - Memory by component
- `milvus_index_memory_bytes` - Memory by index type
- `milvus_segment_memory_bytes` - Memory by segment
- `milvus_memory_usage_percent` - Usage percentage
- `milvus_memory_growth_bytes_per_hour` - Growth rate for leak detection

### Alert Rules

1. **HighMemoryUsage** (Warning) - >85% for 5 minutes
2. **CriticalMemoryUsage** (Critical) - >90% for 2 minutes
3. **MemoryLeak** (Critical) - >100 MB/hour for 2 hours
4. **RapidMemoryGrowth** (Warning) - >500 MB/hour for 10 minutes
5. **MemoryApproachingLimit** (Critical) - >95% for 1 minute
6. **HeapMemoryHigh** (Warning) - Heap >85% of total
7. **IndexMemoryHigh** (Info) - Index type >50 GB

---

**Status:** Implemented and ready for production use