# RFC-0017: Memory Over-Provisioning Detection

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Architecture Improvements  
**Priority:** Low  
**Complexity:** Low-Medium (2-3 weeks)  
**POC Status:** Designed, not implemented

## Summary

Automated detection of memory over-provisioning by analyzing actual memory usage patterns vs allocated resources. Identifies QueryNodes running at <50% memory utilization and recommends right-sizing. Reduces infrastructure costs by 20-40% through data-driven resource optimization.

**Expected Impact:**
- 20-40% cost reduction from right-sizing
- Automated recommendations (no manual analysis)
- Confidence in downsizing (data-backed decisions)

## Motivation

### Problem Statement

**Common scenario:**
- QueryNode provisioned: 64GB memory
- Actual peak usage: 28GB (44% utilization)
- Waste: 36GB ($400/month wasted)
- **Root cause:** Conservative over-provisioning

**Impact:**
- 30-50% of memory capacity wasted across fleet
- $10K+/month wasted on over-provisioning
- No visibility into optimization opportunities

### Use Cases

**Use Case 1: Cost Optimization**
- 10 QueryNodes × 64GB = 640GB provisioned
- Actual usage: 280GB average
- **Recommendation: 10 × 32GB = 320GB**
- **Savings: 50% ($5K/month)**

**Use Case 2: Right-Sizing New Deployments**
- Initial deployment: Conservative 128GB
- After 1 week: Analysis shows 48GB peak
- **Recommendation: 64GB (50% reduction)**

## Detailed Design

**Location:** `tools/capacity_analyzer.py` (new)

```python
#!/usr/bin/env python3

class CapacityAnalyzer:
    """Detect over-provisioning and recommend right-sizing"""
    
    def analyze_node(self, node_id, days=7):
        # Fetch memory metrics for last N days
        metrics = self.fetch_metrics(node_id, days)
        
        allocated = metrics['allocated_memory_gb']
        p95_usage = metrics['p95_memory_gb']
        p99_usage = metrics['p99_memory_gb']
        avg_usage = metrics['avg_memory_gb']
        
        # Calculate utilization
        utilization = p95_usage / allocated
        
        # Detect over-provisioning (< 60% p95 utilization)
        if utilization < 0.60:
            # Recommend size: p99 usage + 20% headroom
            recommended = p99_usage * 1.2
            
            savings = allocated - recommended
            savings_pct = (savings / allocated) * 100
            
            return {
                'node_id': node_id,
                'status': 'over_provisioned',
                'allocated_gb': allocated,
                'p95_usage_gb': p95_usage,
                'p99_usage_gb': p99_usage,
                'utilization_pct': utilization * 100,
                'recommended_gb': recommended,
                'savings_gb': savings,
                'savings_pct': savings_pct,
                'confidence': 'high' if days >= 7 else 'medium'
            }
        
        return {'status': 'optimal'}

# CLI usage
$ python tools/capacity_analyzer.py --analyze-cluster --days 30

Analyzing cluster memory utilization (30 days)...

Over-Provisioned Nodes (5 found):

1. QueryNode-1
   Allocated: 64GB
   P95 Usage: 28GB (44% utilization)
   P99 Usage: 32GB
   Recommendation: Resize to 40GB
   Savings: 24GB ($260/month)
   Confidence: High

2. QueryNode-3
   Allocated: 128GB
   P95 Usage: 52GB (41% utilization)
   P99 Usage: 58GB
   Recommendation: Resize to 72GB
   Savings: 56GB ($610/month)
   Confidence: High

Total Potential Savings: $2,450/month (38% reduction)
```

## Expected Impact

- **20-40% cost reduction** cluster-wide
- **Data-driven decisions** (no guesswork)
- **Automated recommendations** (no manual analysis)

## Drawbacks

1. **Requires Historical Data** - Need ≥7 days of metrics
2. **Conservative Estimates** - May recommend higher than necessary
3. **Doesn't Account for Growth** - Future traffic increases

## References

- Kubernetes Vertical Pod Autoscaler patterns
- AWS Cost Optimization best practices

---

**Status:** Ready for implementation - quick win for cost optimization