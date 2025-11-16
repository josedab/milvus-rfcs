# RFC-0006: Segment Pruning Enhancement

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Performance Optimization  
**Priority:** Medium  
**Complexity:** Medium (3-4 weeks)  
**POC Status:** Designed, not implemented

## Summary

Enhance segment-level pruning using metadata statistics (min/max bounds, clustering info) to skip segments that cannot contain query results. Current implementation searches all segments; this wastes CPU on irrelevant data.

**Expected Impact:**
- 20-40% speedup for filtered queries
- Reduced CPU and memory usage
- Better scalability for large collections

## Motivation

### Problem Statement

Current QueryNode searches **all segments** even when filters make some segments irrelevant:

Example:
- Query: `price > 10000`
- Segment 1: price range [100, 5000] ❌ **Skip!**
- Segment 2: price range [8000, 15000] ✅ **Search**
- Segment 3: price range [20000, 50000] ✅ **Search**

**Current:** Searches all 3 segments  
**Optimized:** Searches only 2 segments (33% reduction)

### Use Cases

**Use Case 1: Time-Series Data**
- Query: `timestamp > 2024-01-01`
- Many old segments can be skipped
- **40% speedup**

**Use Case 2: E-commerce Price Filters**
- Query: `price BETWEEN 100 AND 500`
- Skip luxury and budget segments
- **25% speedup**

## Detailed Design

### Metadata Collection

**Location:** `internal/datanode/segment_stats.go` (enhanced)

```go
type SegmentStatistics struct {
    SegmentID int64
    
    // Per-field statistics
    FieldStats map[string]*FieldRange
    
    // Clustering info (if using clustering compaction)
    ClusteringKey string
    ClusteringKeyRange *FieldRange
}

type FieldRange struct {
    Min interface{}
    Max interface{}
    NullCount int64
    DistinctCount int64
}

// Example usage
func (s *Segment) CanMatchFilter(filter string) bool {
    // Parse filter: "price > 10000"
    // Check segment stats: price max = 5000
    // Return: false (can skip this segment)
    
    expr := parseFilter(filter)
    stats := s.Statistics.FieldStats[expr.Field]
    
    switch expr.Operator {
    case ">":
        // If max value < filter value, skip
        if stats.Max < expr.Value {
            return false
        }
    case "<":
        // If min value > filter value, skip
        if stats.Min > expr.Value {
            return false
        }
    case "BETWEEN":
        // If ranges don't overlap, skip
        if stats.Max < expr.ValueMin || stats.Min > expr.ValueMax {
            return false
        }
    }
    
    return true
}
```

### Pruning Logic

**Location:** `internal/querynodev2/delegator.go` (enhanced)

```go
func (d *ShardDelegator) Search(ctx context.Context, req *querypb.SearchRequest) (*internalpb.SearchResults, error) {
    // Get all segments
    allSegments := d.distribution.GetSegments()
    
    // Prune segments based on filter
    activeSegments := []int64{}
    for _, segID := range allSegments {
        segment := d.segments[segID]
        
        if segment.CanMatchFilter(req.GetExpr()) {
            activeSegments = append(activeSegments, segID)
        } else {
            log.Debug("Pruned segment", 
                zap.Int64("segmentID", segID),
                zap.String("reason", "filter mismatch"))
        }
    }
    
    log.Info("Segment pruning",
        zap.Int("total", len(allSegments)),
        zap.Int("active", len(activeSegments)),
        zap.Int("pruned", len(allSegments) - len(activeSegments)))
    
    // Search only active segments
    return d.searchSegments(ctx, req, activeSegments)
}
```

## Expected Performance

| Filter Type | Segments Searched | Segments Pruned | Speedup |
|-------------|------------------|-----------------|---------|
| No filter | 100% | 0% | 1.0x |
| Time range | 60% | 40% | 1.7x |
| Price range | 75% | 25% | 1.3x |
| Combined filters | 50% | 50% | 2.0x |

## Drawbacks

1. **Statistics Overhead** - Need to maintain and update stats
2. **False Negatives** - Conservative pruning (never skip matching data)
3. **Memory** - Additional metadata storage

## Test Plan

### Validation
- Ensure pruning never skips segments with matching data
- Validate correctness with comprehensive test suite

### Performance
- Measure pruning rate for various filter types
- Confirm speedup matches expectations

## References

- Parquet/ORC pruning strategies
- Database partition elimination techniques

---

**Status:** Ready for implementation - straightforward optimization