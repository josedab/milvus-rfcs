# Milvus Memory Capacity Analyzer

A tool for detecting memory over-provisioning in Milvus QueryNodes and recommending right-sizing to optimize infrastructure costs.

**Implements:** [RFC-0017: Memory Over-Provisioning Detection](../rfcs/0017-memory-over-provisioning-detection.md)

## Overview

The Capacity Analyzer analyzes actual memory usage patterns vs allocated resources across QueryNodes and identifies optimization opportunities. It can reduce infrastructure costs by 20-40% through data-driven resource optimization.

## Features

- **Automated Detection**: Identifies QueryNodes running at <60% memory utilization
- **Data-Driven Recommendations**: Suggests right-sizing based on p99 usage + 20% headroom
- **Cost Analysis**: Calculates potential monthly and yearly savings
- **Confidence Levels**: Provides confidence ratings based on analysis duration
- **Flexible Analysis**: Analyze single nodes, multiple nodes, or entire clusters
- **Multiple Output Formats**: Text reports or JSON for automation

## Installation

### Prerequisites

The tool requires Python 3.6+ and works with optional dependencies:

```bash
# Optional: Install for enhanced functionality
pip install requests numpy

# requests: Required for Prometheus integration
# numpy: Required for accurate percentile calculations (fallback available)
```

### Basic Setup

```bash
# Make the tool executable
chmod +x tools/capacity_analyzer.py

# Test the installation
python3 tools/capacity_analyzer.py --help
```

## Usage

### Analyze a Single Node

```bash
# Analyze one QueryNode over 7 days
python3 tools/capacity_analyzer.py --node querynode-1 --days 7

# Analyze with custom Prometheus URL
python3 tools/capacity_analyzer.py --node querynode-1 \
  --prometheus http://prometheus.milvus.svc:9090 \
  --days 30
```

### Analyze Multiple Nodes

```bash
# Analyze specific nodes
python3 tools/capacity_analyzer.py \
  --nodes querynode-1 querynode-2 querynode-3 \
  --days 14
```

### Analyze Entire Cluster

```bash
# Analyze all QueryNodes (30 days recommended for high confidence)
python3 tools/capacity_analyzer.py --analyze-cluster --days 30

# Output as JSON for automation
python3 tools/capacity_analyzer.py --analyze-cluster \
  --format json > analysis.json
```

### Custom Thresholds

```bash
# Use 70% utilization threshold instead of default 60%
python3 tools/capacity_analyzer.py --analyze-cluster \
  --threshold 0.70 \
  --days 30
```

## Example Output

```
================================================================================
MILVUS MEMORY CAPACITY ANALYSIS REPORT
================================================================================

Analysis Period: 30 days
Report Generated: 2025-04-15 14:30:22

SUMMARY
--------------------------------------------------------------------------------
Total Nodes Analyzed:     5
Over-Provisioned Nodes:   3
Optimally Sized Nodes:    2
Nodes with Errors:        0

Total Allocated Memory:   416.0 GB
Potential Memory Savings: 156.0 GB (37.5%)
Monthly Cost Savings:     $1,638.00

OVER-PROVISIONED NODES
================================================================================

1. querynode-1
   Allocated:       64.0 GB
   Avg Usage:       22.4 GB
   P95 Usage:       28.0 GB (43.8% utilization)
   P99 Usage:       32.0 GB
   Max Usage:       34.2 GB
   Recommendation:  Resize to 40 GB
   Savings:         24.0 GB ($252.00/month)
   Confidence:      VERY_HIGH

2. querynode-3
   Allocated:       128.0 GB
   Avg Usage:       48.6 GB
   P95 Usage:       52.0 GB (40.6% utilization)
   P99 Usage:       58.0 GB
   Max Usage:       62.1 GB
   Recommendation:  Resize to 72 GB
   Savings:         56.0 GB ($588.00/month)
   Confidence:      VERY_HIGH

RECOMMENDATIONS
================================================================================

1. Review over-provisioned nodes and plan downsizing during maintenance window
2. Monitor memory usage for 1-2 weeks after changes
3. Set up alerts for memory usage >80% to prevent OOM
4. Re-run this analysis monthly to track optimization opportunities

Total Estimated Savings: $1,638.00/month ($19,656.00/year)
```

## How It Works

1. **Data Collection**: Queries Prometheus for `milvus_component_memory_bytes` metrics over the specified time period

2. **Statistical Analysis**: Calculates memory usage percentiles:
   - Average usage
   - P50 (median)
   - P95 (95th percentile)
   - P99 (99th percentile)
   - Maximum usage

3. **Over-Provisioning Detection**: Flags nodes where P95 usage < 60% of allocated memory

4. **Recommendations**: Suggests resizing to `P99 usage × 1.2` (20% headroom) rounded to common memory sizes

5. **Cost Calculation**: Estimates savings based on $10.50/GB/month (configurable)

## Confidence Levels

- **HIGH**: 7-29 days of data
- **VERY_HIGH**: 30+ days of data
- **MEDIUM**: <7 days of data (not recommended for production decisions)

## Integration with Prometheus

The tool expects Prometheus to expose Milvus metrics:

```promql
# Memory usage metric
milvus_component_memory_bytes{component="total", node_id="querynode-1"}
```

Configure your Prometheus URL:
```bash
python3 tools/capacity_analyzer.py --analyze-cluster \
  --prometheus http://your-prometheus:9090
```

## Automation

### Scheduled Analysis

Add to cron for monthly reports:

```cron
# Run capacity analysis on the 1st of each month
0 0 1 * * /usr/bin/python3 /path/to/tools/capacity_analyzer.py \
  --analyze-cluster --days 30 > /var/log/milvus/capacity-$(date +\%Y\%m).txt
```

### JSON Output for Tooling

```bash
# Generate JSON for automated processing
python3 tools/capacity_analyzer.py --analyze-cluster \
  --format json | jq '.summary.total_savings_monthly_usd'
```

## Best Practices

1. **Analysis Duration**: Use 30+ days for production decisions to account for traffic patterns

2. **Maintenance Windows**: Schedule downsizing during low-traffic periods

3. **Gradual Changes**: Downsize in steps (e.g., 128GB → 96GB → 72GB) with monitoring

4. **Growth Planning**: Consider future growth when accepting recommendations

5. **Regular Reviews**: Run monthly to catch new optimization opportunities

6. **Alert Setup**: Configure memory alerts at 80% to prevent OOM after downsizing

## Limitations

1. **Requires Historical Data**: Needs ≥7 days of metrics (30+ recommended)
2. **Conservative Estimates**: 20% headroom may be higher than needed
3. **Doesn't Account for Growth**: Future traffic increases not predicted
4. **Static Analysis**: Doesn't consider seasonal or event-driven spikes

## Troubleshooting

### No Metrics Found

```bash
# Verify Prometheus is accessible
curl http://localhost:9090/api/v1/query?query=up

# Check if Milvus metrics exist
curl "http://localhost:9090/api/v1/query?query=milvus_component_memory_bytes"
```

### Connection Errors

- Ensure Prometheus URL is correct
- Check network connectivity
- Verify authentication if required

### Missing Dependencies

```bash
# Install optional dependencies
pip install requests numpy

# The tool will work without them but with reduced functionality
```

## Related Documentation

- [RFC-0017: Memory Over-Provisioning Detection](../rfcs/0017-memory-over-provisioning-detection.md)
- [RFC-0009: Memory Monitoring Framework](../rfcs/0009-memory-monitoring-framework.md)
- [Milvus Prometheus Metrics Documentation](https://milvus.io/docs/monitor.md)

## Contributing

To extend the capacity analyzer:

1. Add new metrics analysis in `fetch_metrics()`
2. Implement custom recommendation logic in `analyze_node()`
3. Add new output formats in `format_cluster_report()`

## License

Apache License 2.0 (same as Milvus)
