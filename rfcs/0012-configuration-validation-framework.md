# RFC-0012: Configuration Validation Framework

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Developer Experience  
**Priority:** Medium  
**Complexity:** Low-Medium (2-3 weeks)  
**POC Status:** Designed, not implemented

## Summary

Pre-deployment configuration validation framework that catches misconfigurations before they reach production. Validates parameter ranges, compatibility rules, and resource requirements. Prevents common configuration errors that cause runtime failures or degraded performance.

**Expected Impact:**
- 80% reduction in configuration-related incidents
- Faster deployment cycles (catch errors early)
- Better onboarding experience for new users

## Motivation

### Problem Statement

**Common configuration errors:**
- HNSW M=128 (too high, causes OOM)
- IVF nlist=10 for 10M vectors (too low, poor performance)
- Memory budget < index requirements
- Incompatible parameter combinations

**Impact:**
- Production outages from misconfiguration
- Hours debugging configuration issues
- Trial-and-error deployment

### Use Cases

**Use Case 1: Prevent OOM**
- User sets HNSW M=64, efConstruction=1000
- Validation: "Estimated memory 128GB exceeds budget 64GB"
- **Impact: Prevented OOM**

**Use Case 2: Performance Warning**
- User sets IVF nlist=16 for 100M vectors
- Validation: "Warning: nlist too low, recommend ≥10000"
- **Impact: Better initial performance**

## Detailed Design

**Location:** `tools/config_validator.py` (new)

```python
#!/usr/bin/env python3

class ConfigValidator:
    """Validate Milvus configuration before deployment"""
    
    def validate_index_config(self, index_type, params, num_vectors, dimensions, memory_budget_gb):
        errors = []
        warnings = []
        
        if index_type == "HNSW":
            M = params.get("M", 16)
            efConstruction = params.get("efConstruction", 200)
            
            # Validation: M range
            if M > 64:
                errors.append(f"HNSW M={M} too high (max recommended: 64)")
            
            # Validation: Memory estimate
            estimated_memory = num_vectors * (dimensions * 4 + M * 2 * 1.2) / (1024**3)
            if estimated_memory > memory_budget_gb * 0.9:
                errors.append(
                    f"Estimated memory {estimated_memory:.1f}GB exceeds "
                    f"budget {memory_budget_gb}GB"
                )
            
            # Warning: efConstruction vs M ratio
            if efConstruction < M * 10:
                warnings.append(
                    f"efConstruction={efConstruction} should be ≥ {M*10} "
                    f"for good recall"
                )
        
        return {"errors": errors, "warnings": warnings}

# CLI usage
$ python tools/config_validator.py --config milvus.yaml --dry-run

✓ Validating configuration...

❌ Errors found (2):
  1. HNSW M=128 exceeds recommended maximum (64)
  2. Estimated memory 156GB exceeds budget 64GB

⚠️  Warnings (1):
  1. IVF nlist=100 too low for 10M vectors (recommend ≥3162)

Recommendation: Fix errors before deployment
```

## Expected Impact

- **80% fewer config errors** in production
- **Faster troubleshooting** (errors caught pre-deployment)
- **Better defaults** guided by validation

## References

- Kubernetes validation patterns
- Schema validation libraries

---

**Status:** Ready for implementation - high ROI for small effort