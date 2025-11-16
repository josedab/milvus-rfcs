# Tiered Storage Strategy Implementation

This package implements the tiered storage strategy for Milvus as described in RFC-0015.

## Overview

The tiered storage system automatically moves data between memory (hot), SSD (warm), and object storage (cold) based on access patterns. This reduces memory costs by 60-80% for large datasets with skewed access patterns while maintaining performance for frequently accessed data.

## Architecture

### Components

1. **TierManager** - Main orchestration component that manages tiers and migration
2. **AccessTracker** - Tracks segment access patterns (frequency, recency, latency)
3. **Storage Tiers**:
   - **MemoryTier** (Hot) - In-memory storage with <10ms latency
   - **SSDTier** (Warm) - SSD-based storage with <50ms latency
   - **ObjectStorageTier** (Cold) - S3/MinIO storage with <500ms latency
4. **TierMigrator** - Handles background migration between tiers

### Tier Selection Logic

- **Hot Tier**: Accessed within last hour AND >100 accesses
- **Warm Tier**: Accessed within last 24h OR >10 accesses
- **Cold Tier**: Everything else

## Configuration

Add the following configuration to enable tiered storage:

```yaml
queryCoord:
  tieredStorage:
    enabled: true

    hotTier:
      maxMemoryGB: 64

    warmTier:
      enabled: true
      path: "/mnt/ssd"
      maxSizeGB: 512

    coldTier:
      enabled: true
      bucket: "milvus-cold-storage"
      endpoint: "s3.amazonaws.com"

    migration:
      hotThreshold: 3600000000000      # 1 hour in nanoseconds
      warmThreshold: 86400000000000    # 24 hours in nanoseconds
      minAccessCount: 10
      maxWorkers: 4
```

## Usage

### Basic Usage

```go
import "github.com/milvus-io/milvus/internal/querycoordv2/tiering"

// Create tier manager
config := &tiering.TierManagerConfig{
    Enabled:             true,
    HotTierMaxMemoryGB:  64,
    WarmTierEnabled:     true,
    WarmTierPath:        "/mnt/ssd",
    WarmTierMaxSizeGB:   512,
    ColdTierEnabled:     true,
    ColdTierBucket:      "milvus-cold-storage",
    ColdTierEndpoint:    "s3.amazonaws.com",
    MaxMigrationWorkers: 4,
}

tm := tiering.NewTierManager(config)
tm.Start()
defer tm.Stop()

// Record segment access
tm.RecordAccess(segmentID, bytesRead, latency)

// Determine optimal tier
tier := tm.DetermineTier(segmentID)

// Manually trigger migration
err := tm.MigrateSegment(segmentID, tiering.TierWarm)

// Get statistics
stats := tm.GetTierStatistics()
```

### Integration with Query Coordinator

The TierManager should be initialized in the QueryCoord server:

```go
// In querycoordv2/server.go
func (s *Server) Start() error {
    // ... existing code ...

    // Initialize tier manager
    if params.QueryCoordCfg.TieredStorageEnabled.GetAsBool() {
        tierConfig := &tiering.TierManagerConfig{
            Enabled:             true,
            HotTierMaxMemoryGB:  params.QueryCoordCfg.TieredStorageHotMaxMemoryGB.GetAsInt64(),
            WarmTierEnabled:     params.QueryCoordCfg.TieredStorageWarmEnabled.GetAsBool(),
            WarmTierPath:        params.QueryCoordCfg.TieredStorageWarmPath.GetValue(),
            WarmTierMaxSizeGB:   params.QueryCoordCfg.TieredStorageWarmMaxSizeGB.GetAsInt64(),
            ColdTierEnabled:     params.QueryCoordCfg.TieredStorageColdEnabled.GetAsBool(),
            ColdTierBucket:      params.QueryCoordCfg.TieredStorageColdBucket.GetValue(),
            ColdTierEndpoint:    params.QueryCoordCfg.TieredStorageColdEndpoint.GetValue(),
            MaxMigrationWorkers: int(params.QueryCoordCfg.TieredStorageMaxMigrationWorkers.GetAsInt()),
        }

        s.tierManager = tiering.NewTierManager(tierConfig)
        if err := s.tierManager.Start(); err != nil {
            return err
        }
    }

    return nil
}
```

## Expected Performance Impact

- **Memory Reduction**: 60-80% for datasets with skewed access patterns
- **Hot Data Latency**: <50ms P95 (maintained)
- **Warm Data Latency**: <50ms P95
- **Cold Data Latency**: <500ms (first access), then cached to warm tier

## Monitoring

Use `GetTierStatistics()` to monitor:
- Capacity and usage for each tier
- Migration statistics (pending, running, completed, failed)
- Access patterns per segment

## Limitations

1. **Cold Start**: First access to cold data is slow (500ms)
2. **Migration Overhead**: Background data movement consumes resources
3. **Capacity Planning**: Requires proper configuration of tier sizes

## Testing

Run the tests:
```bash
go test ./internal/querycoordv2/tiering/...
```

## Future Enhancements

- Machine learning-based tier prediction
- Adaptive threshold adjustment based on workload
- Partial segment migration
- Prefetching for predictable access patterns
