# RFC-0015: Tiered Storage Strategy

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Advanced Features  
**Priority:** Medium  
**Complexity:** Very High (8-10 weeks)  
**POC Status:** Deferred (requires extensive design)

## Summary

Implement hot/warm/cold tiered storage strategy that automatically moves data between memory, SSD, and object storage based on access patterns. Reduces memory costs by 60-80% for large datasets with skewed access patterns while maintaining performance for hot data.

**Expected Impact:**
- 60-80% memory cost reduction for large datasets
- Maintain <50ms latency for hot data
- Automatic tier migration based on access patterns
- Better scalability for billion-scale deployments

## Motivation

### Problem Statement

**Current limitation:** All data in memory
- HNSW index for 100M vectors = 300GB memory
- Cost: $2000/month for memory
- 80% of queries hit 20% of data (Pareto principle)

**Opportunity:**
- Hot data (20%): Keep in memory → fast
- Warm data (30%): Move to SSD → acceptable latency
- Cold data (50%): Move to S3 → rare access OK

**Result:** $400/month instead of $2000 (80% savings)

### Use Cases

**Use Case 1: E-commerce Product Search**
- New products: Hot (searched frequently)
- Seasonal products: Warm (periodic access)
- Discontinued products: Cold (rarely searched)
- **Impact: 75% cost reduction**

**Use Case 2: Document Archive**
- Recent docs: Hot (active projects)
- 6-month old: Warm (occasional reference)
- >1 year old: Cold (compliance only)
- **Impact: 80% cost reduction**

## Detailed Design

### Architecture Overview

```mermaid
graph TB
    subgraph "Storage Tiers"
        HOT[Hot Tier<br/>Memory<br/>< 10ms latency]
        WARM[Warm Tier<br/>SSD<br/>< 50ms latency]
        COLD[Cold Tier<br/>S3/MinIO<br/>< 500ms latency]
    end
    
    subgraph "Access Tracking"
        AT[AccessTracker]
        HM[Heatmap]
    end
    
    subgraph "Migration Engine"
        ME[MigrationEngine]
        POL[TieringPolicy]
    end
    
    Query --> AT
    AT --> HM
    
    HM --> ME
    POL --> ME
    
    ME -->|Promote| WARM
    ME -->|Promote| HOT
    ME -->|Demote| WARM
    ME -->|Demote| COLD
    
    WARM -->|Cache hit| HOT
    COLD -->|On demand| WARM
```

### Implementation Sketch

**Location:** `internal/querycoordv2/tiering/tier_manager.go` (new)

```go
package tiering

type TierManager struct {
    hotTier    *MemoryTier
    warmTier   *SSDTier
    coldTier   *ObjectStorageTier
    
    accessTracker *AccessTracker
    migrator      *TierMigrator
}

type AccessTracker struct {
    segmentAccess map[int64]*AccessStats
}

type AccessStats struct {
    LastAccess    time.Time
    AccessCount   int64
    BytesRead     int64
    AvgLatency    time.Duration
}

func (tm *TierManager) DetermineTier(segmentID int64) StorageTier {
    stats := tm.accessTracker.GetStats(segmentID)
    
    // Hot: accessed in last hour AND >100 times
    if time.Since(stats.LastAccess) < 1*time.Hour && stats.AccessCount > 100 {
        return TierHot
    }
    
    // Warm: accessed in last 24h OR >10 times
    if time.Since(stats.LastAccess) < 24*time.Hour || stats.AccessCount > 10 {
        return TierWarm
    }
    
    // Cold: everything else
    return TierCold
}

func (tm *TierManager) MigrateSegment(segmentID int64, targetTier StorageTier) error {
    // Migrate segment between tiers
    // This is a background operation
    return tm.migrator.Migrate(segmentID, targetTier)
}
```

### Configuration

```yaml
queryNode:
  tieredStorage:
    enabled: true
    
    hotTier:
      maxMemoryGB: 64
      policy: "LRU"
    
    warmTier:
      enabled: true
      path: "/mnt/ssd"
      maxSizeGB: 512
    
    coldTier:
      enabled: true
      type: "s3"
      bucket: "milvus-cold-storage"
      endpoint: "s3.amazonaws.com"
    
    migration:
      hotThreshold: 1h       # Not accessed → demote from hot
      warmThreshold: 24h     # Not accessed → demote from warm
      minAccessCount: 10     # Minimum accesses for hot tier
```

## Expected Impact

- **60-80% memory reduction** for skewed workloads
- **Maintain <50ms P95** for hot data
- **Automatic optimization** (no manual tier management)

## Drawbacks

1. **Complexity** - Very high implementation burden
2. **Cold Start** - First access to cold data is slow
3. **Migration Overhead** - Background data movement costs

## Implementation Phases

**Phase 1:** Access tracking (2 weeks)
**Phase 2:** SSD tier implementation (3 weeks)
**Phase 3:** S3 tier implementation (2 weeks)
**Phase 4:** Auto-migration logic (3 weeks)

## References

- Alluxio tiered storage design
- AWS S3 Intelligent-Tiering
- Blog: Large-scale storage optimization patterns

---

**Status:** Deferred - requires extensive design and validation