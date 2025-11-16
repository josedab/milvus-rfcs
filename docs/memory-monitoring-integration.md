# Memory Monitoring Framework Integration Guide

This guide explains how to integrate the memory monitoring framework into Milvus components.

## Overview

The memory monitoring framework (RFC-0009) provides automatic memory tracking, leak detection, and alerting capabilities. It runs as a background goroutine that periodically collects and reports memory metrics.

## Quick Start

### 1. Import the Package

```go
import (
    "github.com/milvus-io/milvus/pkg/v2/util/hardware"
)
```

### 2. Initialize and Start the Monitor

In your component's initialization code (e.g., `Start()` or `Init()` method):

```go
type MyComponent struct {
    memoryMonitor *hardware.MemoryMonitor
    // ... other fields
}

func (c *MyComponent) Start() error {
    // Initialize memory monitor
    c.memoryMonitor = hardware.NewMemoryMonitor()

    // Start monitoring
    c.memoryMonitor.Start()

    // ... rest of initialization
    return nil
}
```

### 3. Stop the Monitor on Shutdown

In your component's shutdown code:

```go
func (c *MyComponent) Stop() error {
    // Stop memory monitoring
    if c.memoryMonitor != nil {
        c.memoryMonitor.Stop()
    }

    // ... rest of shutdown
    return nil
}
```

## Advanced Usage

### Recording Index Memory

When loading or creating indexes, record their memory usage:

```go
import "github.com/milvus-io/milvus/pkg/v2/util/hardware"

func (qn *QueryNode) LoadIndex(indexType string, collectionID int64) error {
    // ... load index

    // Calculate index memory usage
    indexMemory := calculateIndexMemory(index)

    // Record the memory usage
    hardware.RecordIndexMemory(indexType, collectionID, indexMemory)

    return nil
}
```

### Recording Segment Memory

When loading segments, track their memory footprint:

```go
import "github.com/milvus-io/milvus/pkg/v2/util/hardware"

func (qn *QueryNode) LoadSegment(segmentID, collectionID int64) error {
    // ... load segment

    // Calculate segment memory usage
    segmentMemory := calculateSegmentMemory(segment)

    // Record the memory usage
    hardware.RecordSegmentMemory(segmentID, collectionID, segmentMemory)

    return nil
}
```

### Updating Memory Metrics

Update metrics when memory usage changes (e.g., segment unloading):

```go
func (qn *QueryNode) ReleaseSegment(segmentID, collectionID int64) error {
    // ... release segment

    // Set memory to 0 when segment is released
    hardware.RecordSegmentMemory(segmentID, collectionID, 0)

    return nil
}
```

## Integration Points

### QueryNode

The QueryNode should integrate memory monitoring to track:
- Loaded segment memory
- Index memory (HNSW, IVF_FLAT, etc.)
- Search result cache memory

**Example**:
```go
// In querynode/query_node.go
func (node *QueryNode) Start() error {
    // ... existing initialization

    // Start memory monitoring
    node.memoryMonitor = hardware.NewMemoryMonitor()
    node.memoryMonitor.Start()

    return nil
}

// Track segment loading
func (node *QueryNode) LoadSegments(req *querypb.LoadSegmentsRequest) error {
    for _, segment := range req.Infos {
        // Load segment...
        loadedSegment := node.loader.LoadSegment(segment)

        // Record memory usage
        hardware.RecordSegmentMemory(
            segment.SegmentID,
            segment.CollectionID,
            loadedSegment.MemorySize(),
        )
    }
    return nil
}
```

### DataNode

The DataNode should track:
- Flush buffer memory
- Binlog cache memory
- Insert buffer memory

**Example**:
```go
// In datanode/data_node.go
func (node *DataNode) Start() error {
    // ... existing initialization

    // Start memory monitoring
    node.memoryMonitor = hardware.NewMemoryMonitor()
    node.memoryMonitor.Start()

    return nil
}
```

### IndexNode

The IndexNode should track:
- Index building memory
- Temporary index data

**Example**:
```go
// In indexnode/index_node.go
func (node *IndexNode) Start() error {
    // ... existing initialization

    // Start memory monitoring
    node.memoryMonitor = hardware.NewMemoryMonitor()
    node.memoryMonitor.Start()

    return nil
}

func (node *IndexNode) CreateIndex(req *indexpb.CreateIndexRequest) error {
    // Build index...
    index := node.indexBuilder.Build(req)

    // Record index memory
    hardware.RecordIndexMemory(
        req.IndexType,
        req.CollectionID,
        index.MemorySize(),
    )

    return nil
}
```

## Metrics Exposed

After integration, the following metrics will be automatically exposed:

### Component Metrics (Automatic)
- `milvus_component_memory_bytes{component="total"}` - Total allocated memory
- `milvus_component_memory_bytes{component="heap"}` - Heap memory
- `milvus_component_memory_bytes{component="stack"}` - Stack memory
- `milvus_component_memory_bytes{component="gc_sys"}` - GC system memory

### Derived Metrics (Automatic)
- `milvus_memory_usage_percent` - Memory usage percentage
- `milvus_memory_growth_bytes_per_hour` - Growth rate for leak detection

### Manual Metrics (Requires Recording)
- `milvus_index_memory_bytes` - Index memory (call `RecordIndexMemory`)
- `milvus_segment_memory_bytes` - Segment memory (call `RecordSegmentMemory`)

## Configuration

The memory monitor uses these default configurations (defined in `pkg/util/hardware/memory_monitor.go`):

```go
const (
    // Monitoring interval: 10 seconds
    defaultMonitorInterval = 10 * time.Second

    // Memory leak threshold: 100 MB/hour
    memoryLeakThreshold = 100 * 1024 * 1024

    // Warning threshold: 80%
    memoryWarningThreshold = 0.80

    // Critical threshold: 90%
    memoryCriticalThreshold = 0.90
)
```

These can be adjusted by modifying the constants in the source file.

## Monitoring and Alerts

### View Metrics in Prometheus

Access metrics via Prometheus:
```bash
curl 'http://localhost:9090/api/v1/query?query=milvus_component_memory_bytes'
```

### View Dashboard in Grafana

Import the dashboard from `deployments/monitor/grafana/memory-monitoring-dashboard.json`.

### Configure Alerts

Deploy Prometheus alert rules from `deployments/monitor/prometheus/memory-alerts.yml`.

## Troubleshooting

### Metrics Not Appearing

1. Verify the monitor is started:
   ```go
   log.Info("Memory monitor started", zap.Bool("running", node.memoryMonitor != nil))
   ```

2. Check Prometheus is scraping the metrics endpoint:
   ```bash
   curl http://localhost:9091/metrics | grep milvus_component_memory
   ```

3. Verify metrics are registered:
   ```go
   // In your component initialization
   metrics.Register(prometheus.DefaultRegisterer)
   ```

### Memory Leak False Positives

Memory leak detection may trigger false positives during:
- Initial data loading (expected growth)
- Bulk import operations
- Index building

Consider the context when investigating leak alerts.

### High Memory Usage Alerts

When receiving high memory alerts:

1. Check current workload:
   ```bash
   # View active queries
   curl 'http://localhost:9090/api/v1/query?query=milvus_search_latency_count'
   ```

2. Identify memory-heavy components:
   ```bash
   # Check component breakdown
   curl 'http://localhost:9090/api/v1/query?query=milvus_component_memory_bytes'
   ```

3. Take action:
   - Scale out (add more QueryNodes)
   - Release unused segments
   - Clear caches
   - Restart with more memory

## Best Practices

1. **Always start the monitor**: Initialize in every node type (QueryNode, DataNode, IndexNode)
2. **Record granular metrics**: Use `RecordIndexMemory` and `RecordSegmentMemory` for better visibility
3. **Clean up metrics**: Set memory to 0 when releasing resources
4. **Monitor trends**: Use Grafana dashboards to identify patterns
5. **Set appropriate thresholds**: Adjust based on your deployment size
6. **Regular review**: Check memory patterns weekly to catch issues early

## Example: Complete Integration

```go
package querynode

import (
    "context"

    "github.com/milvus-io/milvus/pkg/v2/util/hardware"
    "github.com/milvus-io/milvus/pkg/v2/log"
    "go.uber.org/zap"
)

type QueryNode struct {
    memoryMonitor *hardware.MemoryMonitor
    segments      map[int64]*Segment
    // ... other fields
}

func (node *QueryNode) Start() error {
    log.Info("Starting QueryNode")

    // Initialize and start memory monitoring
    node.memoryMonitor = hardware.NewMemoryMonitor()
    node.memoryMonitor.Start()
    log.Info("Memory monitoring started")

    // ... rest of initialization
    return nil
}

func (node *QueryNode) Stop() error {
    log.Info("Stopping QueryNode")

    // Stop memory monitoring
    if node.memoryMonitor != nil {
        node.memoryMonitor.Stop()
        log.Info("Memory monitoring stopped")
    }

    // ... rest of shutdown
    return nil
}

func (node *QueryNode) LoadSegment(segmentID, collectionID int64) error {
    // Load segment
    segment := node.loader.Load(segmentID)
    node.segments[segmentID] = segment

    // Record segment memory
    hardware.RecordSegmentMemory(segmentID, collectionID, segment.MemorySize())

    log.Info("Segment loaded",
        zap.Int64("segmentID", segmentID),
        zap.Uint64("memoryBytes", segment.MemorySize()))

    return nil
}

func (node *QueryNode) ReleaseSegment(segmentID, collectionID int64) error {
    // Release segment
    delete(node.segments, segmentID)

    // Clear memory metric
    hardware.RecordSegmentMemory(segmentID, collectionID, 0)

    log.Info("Segment released", zap.Int64("segmentID", segmentID))

    return nil
}
```

## References

- RFC-0009: Memory Monitoring Framework
- `pkg/util/hardware/memory_monitor.go` - Implementation
- `pkg/metrics/memory_metrics.go` - Metrics definitions
- `deployments/monitor/grafana/memory-monitoring-dashboard.json` - Grafana dashboard
- `deployments/monitor/prometheus/memory-alerts.yml` - Alert rules
