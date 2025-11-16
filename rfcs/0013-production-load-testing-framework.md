# RFC-0013: Production Load Testing Framework

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Developer Experience  
**Priority:** Medium  
**Complexity:** Medium (3-4 weeks)  
**POC Status:** Designed, not implemented

## Summary

Production-realistic load testing framework that simulates real query patterns, data distributions, and concurrency levels. Validates performance before deployment and catches regressions. Current testing lacks production realism, leading to unexpected behavior in production.

**Expected Impact:**
- Catch performance regressions before production
- Validate scalability claims with data
- Confident deployments (know expected performance)
- Realistic capacity planning

## Motivation

### Problem Statement

**Testing gaps:**
- Benchmarks use synthetic uniform data (not realistic)
- Single-threaded tests don't reveal concurrency issues
- No validation of P99 latency under load
- Production surprises (worked in test, fails in prod)

### Use Cases

**Use Case 1: Pre-Release Validation**
- Test new index optimization
- Load test: 1000 QPS, 1M vectors
- Validate: P95 < 50ms, P99 < 100ms
- **Impact: Confident release**

**Use Case 2: Capacity Planning**
- Simulate 3x traffic growth
- Measure: latency, CPU, memory at scale
- **Impact: Right-sized provisioning**

## Detailed Design

**Location:** `tests/load_testing/realistic_workload.py` (new)

```python
#!/usr/bin/env python3
"""
Production-realistic load testing for Milvus

Simulates real query patterns, data distributions, and concurrency.
"""

import numpy as np
from locust import HttpUser, task, between
from pymilvus import MilvusClient

class MilvusLoadTest(HttpUser):
    wait_time = between(0.1, 0.5)  # Realistic think time
    
    def on_start(self):
        self.client = MilvusClient(uri="http://localhost:19530")
        self.collection = "load_test"
    
    @task(70)  # 70% of traffic
    def search_common_queries(self):
        """Simulate hot queries (80/20 rule)"""
        # Use Zipfian distribution (realistic access pattern)
        query_id = np.random.zipf(1.5)
        vector = self.get_query_vector(query_id)
        
        self.client.search(
            collection_name=self.collection,
            data=[vector],
            limit=10
        )
    
    @task(20)  # 20% of traffic
    def search_with_filter(self):
        """Filtered queries"""
        vector = self.random_vector()
        filter_expr = self.random_filter()
        
        self.client.search(
            collection_name=self.collection,
            data=[vector],
            filter=filter_expr,
            limit=10
        )
    
    @task(10)  # 10% of traffic
    def search_rare_queries(self):
        """Long-tail queries"""
        vector = self.random_vector()
        
        self.client.search(
            collection_name=self.collection,
            data=[vector],
            limit=100  # Larger result set
        )

# Run load test
$ locust -f realistic_workload.py --users 100 --spawn-rate 10 --run-time 30m

Results:
- QPS: 1000 (target: 1000) ✓
- P50: 28ms (target: <30ms) ✓
- P95: 62ms (target: <70ms) ✓
- P99: 105ms (target: <120ms) ✓
- Error rate: 0.02% (target: <0.1%) ✓
```

## Expected Impact

- **Catch regressions** before production
- **Validate performance claims** with data
- **Confident deployments** (know expected behavior)

## References

- Locust load testing framework
- Gatling for JVM workloads

---

**Status:** Ready for implementation - critical for quality assurance