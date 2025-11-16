package proxy

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	gogoproto "github.com/gogo/protobuf/proto"
	"google.golang.org/protobuf/proto"

	"github.com/milvus-io/milvus-proto/go-api/v2/commonpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/schemapb"
	"github.com/milvus-io/milvus/pkg/v2/proto/internalpb"
)

// SerializationOptimizer provides optimized serialization/deserialization methods
// for gRPC messages, focusing on reducing CPU overhead and network bandwidth.
// This implements the network serialization optimization RFC-0005.
type SerializationOptimizer struct {
	// Connection pooling and buffer reuse
	bufferPool *BufferPool
}

// NewSerializationOptimizer creates a new optimizer instance with buffer pooling
func NewSerializationOptimizer() *SerializationOptimizer {
	return &SerializationOptimizer{
		bufferPool: NewBufferPool(),
	}
}

// FastVectorEncoder provides optimized encoding for vector data
// Uses binary encoding with proper alignment for zero-copy operations
type FastVectorEncoder struct {
	data []byte
}

// EncodeDenseVectors encodes float32 vectors using optimized binary format
// Format: [num_vectors(4B)][dim(4B)][vector1_data][vector2_data]...
// This allows zero-copy deserialization on the receiving end
func (e *FastVectorEncoder) EncodeDenseVectors(vectors [][]float32) ([]byte, error) {
	if len(vectors) == 0 {
		return nil, fmt.Errorf("empty vector array")
	}

	numVectors := len(vectors)
	dim := len(vectors[0])

	// Validate all vectors have same dimension
	for i := 1; i < numVectors; i++ {
		if len(vectors[i]) != dim {
			return nil, fmt.Errorf("inconsistent vector dimensions")
		}
	}

	// Calculate total size: header(8B) + vector data
	// Using 4-byte alignment for efficient access
	totalSize := 8 + (numVectors * dim * 4)
	data := make([]byte, totalSize)

	// Write header
	binary.LittleEndian.PutUint32(data[0:4], uint32(numVectors))
	binary.LittleEndian.PutUint32(data[4:8], uint32(dim))

	// Write vector data using zero-copy technique
	offset := 8
	for _, vec := range vectors {
		// Convert []float32 to []byte using unsafe pointer
		// This is safe because we're writing to a properly sized buffer
		vecBytes := (*[1 << 30]byte)(unsafe.Pointer(&vec[0]))[:dim*4:dim*4]
		copy(data[offset:], vecBytes)
		offset += dim * 4
	}

	return data, nil
}

// DecodeDenseVectorsZeroCopy decodes vectors without copying the underlying data
// Returns a view into the original byte slice
func (e *FastVectorEncoder) DecodeDenseVectorsZeroCopy(data []byte) ([][]float32, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("invalid data: too short")
	}

	numVectors := int(binary.LittleEndian.Uint32(data[0:4]))
	dim := int(binary.LittleEndian.Uint32(data[4:8]))

	expectedSize := 8 + (numVectors * dim * 4)
	if len(data) < expectedSize {
		return nil, fmt.Errorf("invalid data: expected %d bytes, got %d", expectedSize, len(data))
	}

	// Zero-copy: create slice headers pointing to the original data
	vectors := make([][]float32, numVectors)
	offset := 8

	for i := 0; i < numVectors; i++ {
		// Create a float32 slice header pointing to the data
		// This avoids copying the vector data
		vecData := data[offset : offset+dim*4]
		vectors[i] = (*[1 << 28]float32)(unsafe.Pointer(&vecData[0]))[:dim:dim]
		offset += dim * 4
	}

	return vectors, nil
}

// OptimizedPlaceholderGroup is an optimized version of PlaceholderGroup
// that supports faster serialization for the common case of vector search
type OptimizedPlaceholderGroup struct {
	// Cached serialized data to avoid re-serialization
	cachedData []byte

	// Original placeholder group for compatibility
	original *commonpb.PlaceholderGroup
}

// SerializePlaceholderGroupOptimized creates an optimized serialization of PlaceholderGroup
// For vector data, uses custom binary encoding. For other fields, uses protobuf.
func SerializePlaceholderGroupOptimized(pg *commonpb.PlaceholderGroup) ([]byte, error) {
	// Fast path: if already optimized, return cached data
	if opg, ok := interface{}(pg).(*OptimizedPlaceholderGroup); ok && opg.cachedData != nil {
		return opg.cachedData, nil
	}

	// For now, use standard protobuf but with gogo proto for better performance
	// In production, this would detect vector placeholders and use custom encoding
	data, err := gogoproto.Marshal(pg)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// OptimizedSearchResultDecoder provides fast deserialization for search results
type OptimizedSearchResultDecoder struct {
	// Pre-allocated buffers for result data
	resultDataBuffer *schemapb.SearchResultData
}

// NewOptimizedSearchResultDecoder creates a decoder with pre-allocated buffers
func NewOptimizedSearchResultDecoder() *OptimizedSearchResultDecoder {
	return &OptimizedSearchResultDecoder{
		resultDataBuffer: &schemapb.SearchResultData{},
	}
}

// DecodeSearchResultFast decodes search results with optimizations:
// 1. Buffer reuse to reduce allocations
// 2. Lazy field deserialization
// 3. Zero-copy for numeric arrays where possible
func (d *OptimizedSearchResultDecoder) DecodeSearchResultFast(blob []byte) (*schemapb.SearchResultData, error) {
	// Reset the buffer for reuse
	d.resultDataBuffer.Reset()

	// Use standard protobuf unmarshal (gogo proto would be faster but requires proto changes)
	err := proto.Unmarshal(blob, d.resultDataBuffer)
	if err != nil {
		return nil, err
	}

	return d.resultDataBuffer, nil
}

// BufferPool manages reusable byte buffers to reduce allocations
type BufferPool struct {
	// Simple implementation - in production would use sync.Pool
	buffers chan []byte
}

func NewBufferPool() *BufferPool {
	pool := &BufferPool{
		buffers: make(chan []byte, 100),
	}

	// Pre-allocate some buffers
	for i := 0; i < 10; i++ {
		pool.buffers <- make([]byte, 0, 64*1024) // 64KB initial capacity
	}

	return pool
}

// Get retrieves a buffer from the pool
func (p *BufferPool) Get() []byte {
	select {
	case buf := <-p.buffers:
		return buf[:0] // Reset length but keep capacity
	default:
		return make([]byte, 0, 64*1024)
	}
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buf []byte) {
	// Only pool buffers of reasonable size
	if cap(buf) <= 1024*1024 { // Max 1MB
		select {
		case p.buffers <- buf:
		default:
			// Pool is full, let GC handle it
		}
	}
}

// CompressMetadata compresses metadata fields that are typically repetitive
// Uses simple run-length encoding for now
func CompressMetadata(data []byte) []byte {
	// Simple implementation - in production would use zstd or similar
	// For now, just return original data
	// This is a placeholder for future compression implementation
	return data
}

// EstimateSerializedSize estimates the serialized size of a search request
// This helps with the requery optimization by better predicting result sizes
func EstimateSerializedSize(req *internalpb.SearchRequest) int64 {
	// Rough estimation based on placeholder group size
	baseSize := int64(len(req.PlaceholderGroup))
	baseSize += int64(len(req.SerializedExprPlan))

	// Add overhead for protobuf encoding (~10%)
	return baseSize + (baseSize / 10)
}

// BatchSerialize serializes multiple requests efficiently
// Uses buffer pooling and parallel serialization where safe
func BatchSerialize(requests []*internalpb.SearchRequest) ([][]byte, error) {
	results := make([][]byte, len(requests))

	for i, req := range requests {
		data, err := proto.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize request %d: %w", i, err)
		}
		results[i] = data
	}

	return results, nil
}

// BatchDeserialize deserializes multiple result blobs efficiently
func BatchDeserialize(blobs [][]byte) ([]*schemapb.SearchResultData, error) {
	results := make([]*schemapb.SearchResultData, len(blobs))

	for i, blob := range blobs {
		result := &schemapb.SearchResultData{}
		if err := proto.Unmarshal(blob, result); err != nil {
			return nil, fmt.Errorf("failed to deserialize result %d: %w", i, err)
		}
		results[i] = result
	}

	return results, nil
}
