# gRPC Network Serialization Optimization

This document describes the implementation of RFC-0005: Network Serialization Optimization.

## Overview

The gRPC optimizer provides optimized serialization/deserialization for Milvus proxy operations, specifically targeting:
- Search request handling with large vector payloads
- Search result deserialization
- Metadata encoding/decoding

## Components

### SerializationOptimizer
Main optimizer class that coordinates buffer pooling and provides the optimization framework.

### FastVectorEncoder
Optimized encoder for dense vector data using:
- **Binary encoding** with proper alignment for efficient memory access
- **Zero-copy deserialization** that creates slice headers pointing directly to buffer data
- **Format**: `[num_vectors(4B)][dim(4B)][vector1_data][vector2_data]...`

**Usage Example**:
```go
encoder := &FastVectorEncoder{}

// Encode vectors
vectors := [][]float32{
    {1.0, 2.0, 3.0, 4.0},
    {5.0, 6.0, 7.0, 8.0},
}
encoded, err := encoder.EncodeDenseVectors(vectors)

// Decode with zero-copy
decoded, err := encoder.DecodeDenseVectorsZeroCopy(encoded)
```

### OptimizedSearchResultDecoder
Fast decoder for search results that:
- Reuses buffers to reduce allocations
- Implements lazy field deserialization
- Supports zero-copy for numeric arrays

**Usage Example**:
```go
decoder := NewOptimizedSearchResultDecoder()
result, err := decoder.DecodeSearchResultFast(blob)
```

### BufferPool
Memory buffer pooling to reduce GC pressure:
- Pre-allocates 10 buffers of 64KB each
- Automatically manages buffer lifecycle
- Caps maximum buffer size at 1MB to prevent memory bloat

**Usage Example**:
```go
pool := NewBufferPool()
buf := pool.Get()
// ... use buffer ...
pool.Put(buf)
```

## Performance Expectations

Based on RFC-0005, the optimizations target:

| Operation | Current (ms) | Optimized (ms) | Improvement |
|-----------|--------------|----------------|-------------|
| 100 vector serialization | 12 | 10 | 17% |
| Result deserialization | 8 | 6 | 25% |
| Metadata encoding | 5 | 3 | 40% |

**Overall Impact**:
- 10-15% reduction in query latency
- 20% reduction in network bandwidth (with compression)
- Lower CPU usage on Proxy and QueryNode

## Integration Points

### Current Integration
The optimizer is designed to integrate with existing code at:

1. **Search request preparation** (`task_search.go`):
   - PlaceholderGroup serialization (line ~459, 496)
   - Plan serialization (line ~514, 626)

2. **Search result processing** (`search_reduce_util.go`):
   - Result blob deserialization (line ~536-554)

3. **Batch operations** (`search_pipeline.go`):
   - Multiple request/result handling

### Future Integration (Recommended)

To fully leverage these optimizations:

1. **Replace PlaceholderGroup serialization**:
   ```go
   // In task_search.go, replace:
   // req.PlaceholderGroup = placeholderGroupBytes

   // With:
   optimized, err := SerializePlaceholderGroupOptimized(placeholderGroup)
   req.PlaceholderGroup = optimized
   ```

2. **Use optimized result decoder**:
   ```go
   // In search_reduce_util.go, replace:
   // proto.Unmarshal(partialSearchResult.SlicedBlob, &partialResultData)

   // With:
   decoder := NewOptimizedSearchResultDecoder()
   partialResultData, err := decoder.DecodeSearchResultFast(partialSearchResult.SlicedBlob)
   ```

3. **Leverage batch operations**:
   ```go
   // For multiple sub-requests:
   blobs, err := BatchSerialize(subRequests)
   ```

## Benchmarks

Run benchmarks to measure performance:

```bash
# Run all optimization benchmarks
go test -bench=. -benchmem ./internal/proxy/grpc_optimizer_test.go

# Specific benchmarks
go test -bench=BenchmarkVectorEncoding -benchmem
go test -bench=BenchmarkVectorDecoding -benchmem
go test -bench=BenchmarkSearchResultDecoding -benchmem
```

Example output (expected):
```
BenchmarkVectorEncoding-8              5000    250000 ns/op    307200 B/op    1 allocs/op
BenchmarkVectorDecoding-8             20000     60000 ns/op        24 B/op    1 allocs/op
BenchmarkSearchResultDecoding-8       10000    150000 ns/op     50000 B/op   15 allocs/op
```

## Implementation Notes

### Zero-Copy Safety
The zero-copy implementation uses `unsafe.Pointer` to create slice headers that point directly to the underlying byte buffer. This is safe because:
1. The buffer lifetime is controlled and exceeds the slice lifetime
2. Alignment is guaranteed by the encoding format (4-byte aligned float32)
3. Bounds are checked during decoding

### Buffer Pool Design
The current implementation uses a simple buffered channel. For production:
- Consider `sync.Pool` for better concurrency
- Add metrics for pool hit/miss rates
- Implement size-based pooling (multiple pools for different sizes)

### Gogoproto Integration
The code imports `github.com/gogo/protobuf/proto` for faster marshaling. To fully leverage:
1. Regenerate protobuf files with gogoproto annotations
2. Enable `gogoproto.marshaler` and `gogoproto.unmarshaler` for hot types
3. Use `gogoproto.nullable = false` for required fields

## Testing

Run tests:
```bash
# Unit tests
go test ./internal/proxy/grpc_optimizer_test.go -v

# Specific tests
go test -run TestFastVectorEncoder
go test -run TestOptimizedSearchResultDecoder
go test -run TestBufferPool
```

## Monitoring

Add metrics to track optimization effectiveness:
- Serialization/deserialization latency
- Buffer pool hit rate
- Memory allocation reduction
- Network bandwidth savings

Recommended metrics locations:
- `internal/proxy/task_search.go` - Request serialization time
- `internal/proxy/search_reduce_util.go` - Result deserialization time

## Future Enhancements

1. **Compression**: Implement zstd compression for metadata fields
2. **Connection pooling**: Optimize gRPC connection reuse
3. **Adaptive encoding**: Switch between formats based on data characteristics
4. **SIMD optimization**: Use assembly for vector encoding on supported platforms
5. **Protocol changes**: Update `.proto` files to use gogoproto for all hot types

## References

- RFC-0005: Network Serialization Optimization
- [gRPC Performance Best Practices](https://grpc.io/docs/guides/performance/)
- [Protocol Buffers Encoding Guide](https://protobuf.dev/programming-guides/encoding/)
- [gogoproto Documentation](https://github.com/gogo/protobuf)
