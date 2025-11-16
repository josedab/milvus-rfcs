# Milvus Memory Monitoring Framework

This directory contains Prometheus alerting rules for Milvus memory monitoring framework as specified in RFC-0009.

## Overview

The memory monitoring framework provides comprehensive tracking and alerting for:
- Component-level memory usage (total, heap, stack, GC)
- Index-specific memory consumption
- Segment memory tracking
- Memory leak detection
- OOM prevention through early warning

## Alert Rules

### HighMemoryUsage (Warning)
- **Threshold**: 85% memory usage for 5 minutes
- **Action**: Monitor the situation, prepare to scale or reduce load
- **Impact**: May lead to performance degradation

### CriticalMemoryUsage (Critical)
- **Threshold**: 90% memory usage for 2 minutes
- **Action**: Immediate intervention required - consider scaling out or reducing workload
- **Impact**: OOM condition imminent

### MemoryLeak (Critical)
- **Threshold**: Memory growing >100 MiB/hour for 2 hours continuously
- **Action**: Investigate application for memory leaks, check for unbounded caches
- **Impact**: Will eventually lead to OOM if not addressed

### RapidMemoryGrowth (Warning)
- **Threshold**: Memory growing >500 MiB/hour for 10 minutes
- **Action**: Investigate unusual workload or activity
- **Impact**: May indicate spike in traffic or data loading

### MemoryApproachingLimit (Critical, Page)
- **Threshold**: 95% memory usage for 1 minute
- **Action**: Emergency response required - OOM killer may trigger
- **Impact**: Service disruption imminent

### HeapMemoryHigh (Warning)
- **Threshold**: Heap memory >85% of total for 5 minutes
- **Action**: Check GC pressure, tune GOGC, investigate allocations
- **Impact**: May indicate inefficient memory management

### IndexMemoryHigh (Info)
- **Threshold**: Single index type using >50 GiB
- **Action**: Consider optimizing index parameters or distributing load
- **Impact**: Informational - helps with capacity planning

## Deployment

### Prometheus Configuration

Add the following to your Prometheus configuration:

```yaml
rule_files:
  - "/etc/prometheus/rules/memory-alerts.yml"
```

Then copy the alert rules:

```bash
cp memory-alerts.yml /etc/prometheus/rules/
```

Reload Prometheus configuration:

```bash
curl -X POST http://localhost:9090/-/reload
```

### AlertManager Integration

Configure AlertManager routes for memory alerts:

```yaml
route:
  routes:
    - match:
        component: memory
      receiver: 'memory-alerts'
      group_by: ['node_id', 'severity']
      group_wait: 10s
      group_interval: 10s
      repeat_interval: 1h

receivers:
  - name: 'memory-alerts'
    slack_configs:
      - channel: '#milvus-alerts'
        title: 'Milvus Memory Alert'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

## Metrics Reference

### milvus_component_memory_bytes
Memory usage by component (total, heap, stack, gc_sys).

**Labels**:
- `component`: Component name (total, heap, stack, gc_sys)
- `node_id`: Milvus node ID

### milvus_index_memory_bytes
Memory usage by index type.

**Labels**:
- `index_type`: Type of index (HNSW, IVF_FLAT, etc.)
- `collection_id`: Collection ID

### milvus_segment_memory_bytes
Memory usage by segment.

**Labels**:
- `segment_id`: Segment ID
- `collection_id`: Collection ID

### milvus_memory_usage_percent
Overall memory usage as percentage of total available memory.

**Labels**:
- `node_id`: Milvus node ID

### milvus_memory_growth_bytes_per_hour
Memory growth rate in bytes per hour (for leak detection).

**Labels**:
- `node_id`: Milvus node ID

## Grafana Dashboard

Import the Grafana dashboard from `../grafana/memory-monitoring-dashboard.json` to visualize:
- Memory usage by component (stacked area chart)
- Memory usage percentage (gauge)
- Memory growth rate (time series)
- Memory usage by index type
- Memory leak detection graph
- Top 10 segments by memory usage

## Testing

### Simulate High Memory Usage

```bash
# Using stress-ng (if available)
stress-ng --vm 1 --vm-bytes 80% --timeout 60s

# Or using Python
python3 -c "x = bytearray(10**9); import time; time.sleep(300)"
```

### Verify Alerts

Check Prometheus alerts:
```bash
curl http://localhost:9090/api/v1/alerts
```

Check AlertManager notifications:
```bash
curl http://localhost:9093/api/v2/alerts
```

## Troubleshooting

### Alerts Not Firing

1. Verify metrics are being collected:
   ```bash
   curl 'http://localhost:9090/api/v1/query?query=milvus_memory_usage_percent'
   ```

2. Check Prometheus rules are loaded:
   ```bash
   curl http://localhost:9090/api/v1/rules
   ```

3. Validate rule syntax:
   ```bash
   promtool check rules memory-alerts.yml
   ```

### False Positives

Adjust thresholds in `memory-alerts.yml` based on your environment:
- Increase memory percentage thresholds for larger deployments
- Adjust leak detection threshold based on expected growth patterns
- Tune `for` duration to reduce noise

## Best Practices

1. **Baseline Establishment**: Monitor memory usage patterns for 1-2 weeks before setting final thresholds
2. **Regular Review**: Review and adjust alerts monthly based on operational experience
3. **Correlation**: Correlate memory alerts with workload metrics (QPS, data volume, etc.)
4. **Capacity Planning**: Use historical memory data to plan capacity 3-6 months ahead
5. **Runbook**: Maintain incident response runbooks for each alert type

## References

- RFC-0009: Memory Monitoring Framework
- [Prometheus Alerting Documentation](https://prometheus.io/docs/alerting/latest/overview/)
- [Grafana Dashboard Best Practices](https://grafana.com/docs/grafana/latest/best-practices/)
