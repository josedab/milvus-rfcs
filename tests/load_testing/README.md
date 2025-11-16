# Milvus Production Load Testing Framework

Production-realistic load testing framework that simulates real query patterns, data distributions, and concurrency levels. This framework helps validate performance before deployment and catch regressions early.

## Overview

This framework uses [Locust](https://locust.io/) to generate realistic workloads that mirror production traffic patterns. It simulates three types of queries with realistic distributions:

1. **Common Queries (70%)** - Hot queries following Zipfian distribution (80/20 rule)
2. **Filtered Queries (20%)** - Vector search with metadata filters
3. **Rare Queries (10%)** - Long-tail queries with larger result sets

## Features

- ✅ **Realistic Query Patterns** - Uses Zipfian distribution to simulate hot/cold query patterns
- ✅ **Configurable Workloads** - Easy customization via environment variables
- ✅ **Performance Validation** - Automatic comparison against target SLAs
- ✅ **Detailed Metrics** - P50, P95, P99 latencies, QPS, error rates
- ✅ **Multiple Scenarios** - Pre-configured scenarios for different testing needs
- ✅ **Web UI & Headless Modes** - Interactive or automated testing

## Quick Start

### 1. Installation

```bash
# Install dependencies
pip install -r requirements.txt
```

### 2. Prepare Test Collection

Before running load tests, you need a Milvus collection with test data. Use the provided setup script:

```bash
# Create and populate test collection
python setup_test_collection.py --num-vectors 1000000 --dimension 768
```

### 3. Run Load Test

**Interactive Mode (Web UI):**
```bash
# Start Locust web UI on http://localhost:8089
locust -f realistic_workload.py
```

**Headless Mode (Automated):**
```bash
# Run 30-minute test with 100 concurrent users
locust -f realistic_workload.py \
    --headless \
    --users 100 \
    --spawn-rate 10 \
    --run-time 30m \
    --csv=results
```

## Configuration

### Environment Variables

All configuration is done via environment variables. See `config.env.example` for all options.

```bash
# Copy example config
cp config.env.example config.env

# Edit configuration
vim config.env

# Run with config
source config.env && locust -f realistic_workload.py
```

### Key Configuration Options

| Variable | Default | Description |
|----------|---------|-------------|
| `MILVUS_URI` | `http://localhost:19530` | Milvus server URI |
| `LOAD_TEST_COLLECTION` | `load_test_collection` | Collection name for testing |
| `VECTOR_DIMENSION` | `768` | Vector dimension |
| `COMMON_QUERY_WEIGHT` | `70` | % of common queries |
| `FILTERED_QUERY_WEIGHT` | `20` | % of filtered queries |
| `RARE_QUERY_WEIGHT` | `10` | % of rare queries |
| `TARGET_QPS` | `1000` | Target queries per second |
| `TARGET_P99_MS` | `120` | Target P99 latency (ms) |

## Test Scenarios

### Scenario 1: Baseline Performance
Establish baseline metrics with light load.

```bash
locust -f realistic_workload.py \
    --headless \
    --users 10 \
    --spawn-rate 2 \
    --run-time 5m
```

### Scenario 2: Normal Production Load
Simulate typical production traffic.

```bash
locust -f realistic_workload.py \
    --headless \
    --users 100 \
    --spawn-rate 10 \
    --run-time 30m \
    --csv=normal_load
```

### Scenario 3: Peak Load
Test 2-3x normal load (traffic spikes).

```bash
locust -f realistic_workload.py \
    --headless \
    --users 300 \
    --spawn-rate 30 \
    --run-time 30m \
    --csv=peak_load
```

### Scenario 4: Stress Test
Push system to limits to find breaking point.

```bash
locust -f realistic_workload.py \
    --headless \
    --users 1000 \
    --spawn-rate 50 \
    --run-time 1h \
    --csv=stress_test
```

### Scenario 5: Endurance Test
Long-running test to detect memory leaks and performance degradation.

```bash
locust -f realistic_workload.py \
    --headless \
    --users 100 \
    --spawn-rate 10 \
    --run-time 4h \
    --csv=endurance_test
```

## Understanding Results

### Example Output

```
Results Summary:
  Total Requests: 150,000
  Total Failures: 25
  Error Rate: 0.02%
  Average Response Time: 35.42ms
  Median Response Time (P50): 28.00ms
  95th Percentile (P95): 62.00ms
  99th Percentile (P99): 105.00ms
  Requests/sec: 1,000.00

Target Validation:
  QPS: 1000 (target: 1000) ✓
  P50: 28ms (target: <30ms) ✓
  P95: 62ms (target: <70ms) ✓
  P99: 105ms (target: <120ms) ✓
  Error rate: 0.02% (target: <0.1%) ✓
```

### Key Metrics

- **QPS (Queries Per Second)** - Throughput achieved
- **P50 (Median)** - Half of requests faster than this
- **P95** - 95% of requests faster than this
- **P99** - 99% of requests faster than this (tail latency)
- **Error Rate** - % of failed requests

### Interpreting Results

✅ **All targets met** - System performing well, ready for deployment

⚠️ **Some targets missed** - Investigate bottlenecks:
- High P99 but good P50 → Check for outliers, GC pauses
- High error rate → Check logs, resource constraints
- Low QPS → CPU/memory bottleneck, network issues

❌ **Multiple targets missed** - System not ready:
- Review resource allocation
- Check index configuration
- Analyze query patterns
- Consider scaling

## Query Patterns

### Common Queries (Zipfian Distribution)

Simulates the 80/20 rule where a small set of queries accounts for most traffic:

```python
# Lower query IDs accessed more frequently
query_id = np.random.zipf(1.5)  # Parameter controls skew
vector = get_hot_vector(query_id)
```

### Filtered Queries

Combines vector search with metadata filtering:

```python
# Example filters
category == "electronics"
price >= 100 and price < 500
category == "books" and price >= 10 and price < 50
```

### Rare Queries

Long-tail queries with larger result sets (100 results vs 10).

## Advanced Usage

### Custom Query Distribution

Modify task weights in code or via environment variables:

```bash
export COMMON_QUERY_WEIGHT=80
export FILTERED_QUERY_WEIGHT=15
export RARE_QUERY_WEIGHT=5
```

### Running Against Remote Cluster

```bash
locust -f realistic_workload.py \
    --host http://milvus-cluster.example.com:19530 \
    --users 500 \
    --spawn-rate 50
```

### Distributed Load Testing

Run Locust in distributed mode for extreme load:

```bash
# Master node
locust -f realistic_workload.py --master

# Worker nodes (run on multiple machines)
locust -f realistic_workload.py --worker --master-host=<master-ip>
```

### Collecting Metrics

```bash
# CSV output for analysis
locust -f realistic_workload.py \
    --headless \
    --users 100 \
    --run-time 30m \
    --csv=results \
    --html=report.html
```

This generates:
- `results_stats.csv` - Request statistics
- `results_stats_history.csv` - Time-series data
- `results_failures.csv` - Failure details
- `report.html` - Visual report

## Troubleshooting

### Collection Not Found

```
Collection 'load_test_collection' does not exist
```

**Solution:** Create collection using `setup_test_collection.py`

### Connection Refused

```
Failed to connect to Milvus: Connection refused
```

**Solution:**
- Check Milvus is running: `docker ps | grep milvus`
- Verify URI: `export MILVUS_URI=http://localhost:19530`

### Low QPS

If actual QPS is much lower than expected:
- Increase `--users` (concurrent users)
- Decrease `wait_time` in code
- Check network latency
- Verify Milvus resource allocation

### High Error Rate

- Check Milvus logs for errors
- Verify resource limits (CPU, memory)
- Check rate limiting configuration
- Monitor system metrics (CPU, memory, disk I/O)

## Best Practices

### Before Running Tests

1. **Warm up the system** - Run short test first to load caches
2. **Verify collection exists** - Ensure test data is loaded
3. **Set realistic targets** - Base on production requirements
4. **Monitor resources** - Watch CPU, memory, network during test

### During Tests

1. **Start small** - Begin with low users, gradually increase
2. **Monitor metrics** - Watch Milvus metrics, system resources
3. **Log everything** - Keep detailed logs for analysis
4. **Multiple runs** - Run tests multiple times for consistency

### After Tests

1. **Analyze results** - Compare against targets and baselines
2. **Identify bottlenecks** - Use profiling tools if needed
3. **Document findings** - Keep history of performance over time
4. **Iterate** - Optimize and retest

## Integration with CI/CD

### GitHub Actions Example

```yaml
name: Load Test

on:
  pull_request:
    branches: [main]

jobs:
  load-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Milvus
        run: |
          docker-compose up -d milvus
          ./wait-for-milvus.sh

      - name: Install dependencies
        run: pip install -r tests/load_testing/requirements.txt

      - name: Create test collection
        run: python tests/load_testing/setup_test_collection.py

      - name: Run load test
        run: |
          cd tests/load_testing
          locust -f realistic_workload.py \
            --headless \
            --users 50 \
            --spawn-rate 10 \
            --run-time 5m \
            --csv=results

      - name: Upload results
        uses: actions/upload-artifact@v3
        with:
          name: load-test-results
          path: tests/load_testing/results*.csv
```

## Contributing

Contributions welcome! Areas for improvement:

- Additional query patterns (batch queries, hybrid search)
- More realistic data distributions
- Performance regression detection
- Integration with monitoring systems (Prometheus, Grafana)

## References

- [Locust Documentation](https://docs.locust.io/)
- [Milvus Documentation](https://milvus.io/docs)
- [RFC-0013: Production Load Testing Framework](../../rfcs/0013-production-load-testing-framework.md)

## License

Same as Milvus project.
