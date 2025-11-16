package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/milvus-io/milvus-proto/go-api/v2/commonpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/schemapb"
	"github.com/milvus-io/milvus/pkg/v2/proto/internalpb"
)

// TestFastVectorEncoder tests the optimized vector encoding/decoding
func TestFastVectorEncoder(t *testing.T) {
	encoder := &FastVectorEncoder{}

	// Test case 1: Normal vectors
	vectors := [][]float32{
		{1.0, 2.0, 3.0, 4.0},
		{5.0, 6.0, 7.0, 8.0},
		{9.0, 10.0, 11.0, 12.0},
	}

	encoded, err := encoder.EncodeDenseVectors(vectors)
	assert.NoError(t, err)
	assert.NotNil(t, encoded)

	// Decode with zero-copy
	decoded, err := encoder.DecodeDenseVectorsZeroCopy(encoded)
	assert.NoError(t, err)
	assert.Equal(t, len(vectors), len(decoded))

	// Verify data integrity
	for i := range vectors {
		assert.Equal(t, len(vectors[i]), len(decoded[i]))
		for j := range vectors[i] {
			assert.Equal(t, vectors[i][j], decoded[i][j])
		}
	}
}

// TestFastVectorEncoderEmptyVectors tests error handling
func TestFastVectorEncoderEmptyVectors(t *testing.T) {
	encoder := &FastVectorEncoder{}

	// Test empty vector array
	_, err := encoder.EncodeDenseVectors([][]float32{})
	assert.Error(t, err)

	// Test inconsistent dimensions
	vectors := [][]float32{
		{1.0, 2.0, 3.0},
		{4.0, 5.0}, // Wrong dimension
	}
	_, err = encoder.EncodeDenseVectors(vectors)
	assert.Error(t, err)
}

// TestOptimizedSearchResultDecoder tests the decoder with buffer reuse
func TestOptimizedSearchResultDecoder(t *testing.T) {
	decoder := NewOptimizedSearchResultDecoder()

	// Create a sample search result
	result := &schemapb.SearchResultData{
		NumQueries: 1,
		TopK:       10,
		Ids: &schemapb.IDs{
			IdField: &schemapb.IDs_IntId{
				IntId: &schemapb.LongArray{
					Data: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
				},
			},
		},
		Scores: []float32{0.9, 0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2, 0.1, 0.0},
	}

	// Serialize
	blob, err := proto.Marshal(result)
	assert.NoError(t, err)

	// Deserialize with optimized decoder
	decoded, err := decoder.DecodeSearchResultFast(blob)
	assert.NoError(t, err)
	assert.Equal(t, result.NumQueries, decoded.NumQueries)
	assert.Equal(t, result.TopK, decoded.TopK)
	assert.Equal(t, len(result.Scores), len(decoded.Scores))
}

// TestBufferPool tests the buffer pool functionality
func TestBufferPool(t *testing.T) {
	pool := NewBufferPool()

	// Get a buffer
	buf1 := pool.Get()
	assert.NotNil(t, buf1)

	// Use the buffer
	buf1 = append(buf1, []byte("test data")...)

	// Return to pool
	pool.Put(buf1)

	// Get another buffer (might be the same one)
	buf2 := pool.Get()
	assert.NotNil(t, buf2)
	// Buffer should be reset
	assert.Equal(t, 0, len(buf2))
}

// TestBatchSerialize tests batch serialization
func TestBatchSerialize(t *testing.T) {
	requests := []*internalpb.SearchRequest{
		{
			Base: &commonpb.MsgBase{
				MsgID: 1,
			},
			CollectionID: 100,
		},
		{
			Base: &commonpb.MsgBase{
				MsgID: 2,
			},
			CollectionID: 200,
		},
	}

	results, err := BatchSerialize(requests)
	assert.NoError(t, err)
	assert.Equal(t, len(requests), len(results))

	// Verify each can be deserialized
	for i, blob := range results {
		req := &internalpb.SearchRequest{}
		err := proto.Unmarshal(blob, req)
		assert.NoError(t, err)
		assert.Equal(t, requests[i].CollectionID, req.CollectionID)
	}
}

// TestBatchDeserialize tests batch deserialization
func TestBatchDeserialize(t *testing.T) {
	// Create sample results
	results := []*schemapb.SearchResultData{
		{
			NumQueries: 1,
			TopK:       5,
			Scores:     []float32{0.9, 0.8, 0.7, 0.6, 0.5},
		},
		{
			NumQueries: 1,
			TopK:       3,
			Scores:     []float32{0.95, 0.85, 0.75},
		},
	}

	// Serialize them
	blobs := make([][]byte, len(results))
	for i, result := range results {
		blob, err := proto.Marshal(result)
		assert.NoError(t, err)
		blobs[i] = blob
	}

	// Batch deserialize
	decoded, err := BatchDeserialize(blobs)
	assert.NoError(t, err)
	assert.Equal(t, len(results), len(decoded))

	// Verify data
	for i := range results {
		assert.Equal(t, results[i].NumQueries, decoded[i].NumQueries)
		assert.Equal(t, results[i].TopK, decoded[i].TopK)
		assert.Equal(t, len(results[i].Scores), len(decoded[i].Scores))
	}
}

// BenchmarkVectorEncoding benchmarks the optimized vector encoding
func BenchmarkVectorEncoding(b *testing.B) {
	encoder := &FastVectorEncoder{}

	// Create test vectors: 100 vectors of 768 dimensions (common for embeddings)
	numVectors := 100
	dim := 768
	vectors := make([][]float32, numVectors)
	for i := 0; i < numVectors; i++ {
		vectors[i] = make([]float32, dim)
		for j := 0; j < dim; j++ {
			vectors[i][j] = float32(i*dim + j)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := encoder.EncodeDenseVectors(vectors)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVectorDecoding benchmarks the zero-copy vector decoding
func BenchmarkVectorDecoding(b *testing.B) {
	encoder := &FastVectorEncoder{}

	// Prepare encoded data
	numVectors := 100
	dim := 768
	vectors := make([][]float32, numVectors)
	for i := 0; i < numVectors; i++ {
		vectors[i] = make([]float32, dim)
		for j := 0; j < dim; j++ {
			vectors[i][j] = float32(i*dim + j)
		}
	}

	encoded, err := encoder.EncodeDenseVectors(vectors)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := encoder.DecodeDenseVectorsZeroCopy(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStandardProtobufMarshal benchmarks standard protobuf marshaling
func BenchmarkStandardProtobufMarshal(b *testing.B) {
	req := &internalpb.SearchRequest{
		Base: &commonpb.MsgBase{
			MsgID: 12345,
		},
		CollectionID:     100,
		PlaceholderGroup: make([]byte, 100*768*4), // ~300KB for 100 768-dim vectors
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := proto.Marshal(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStandardProtobufUnmarshal benchmarks standard protobuf unmarshaling
func BenchmarkStandardProtobufUnmarshal(b *testing.B) {
	req := &internalpb.SearchRequest{
		Base: &commonpb.MsgBase{
			MsgID: 12345,
		},
		CollectionID:     100,
		PlaceholderGroup: make([]byte, 100*768*4),
	}

	data, err := proto.Marshal(req)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := &internalpb.SearchRequest{}
		err := proto.Unmarshal(data, result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchResultDecoding benchmarks optimized result decoding
func BenchmarkSearchResultDecoding(b *testing.B) {
	decoder := NewOptimizedSearchResultDecoder()

	// Create a realistic search result
	result := &schemapb.SearchResultData{
		NumQueries: 10,
		TopK:       100,
		Ids: &schemapb.IDs{
			IdField: &schemapb.IDs_IntId{
				IntId: &schemapb.LongArray{
					Data: make([]int64, 1000),
				},
			},
		},
		Scores: make([]float32, 1000),
	}

	blob, err := proto.Marshal(result)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := decoder.DecodeSearchResultFast(blob)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBatchSerialize benchmarks batch serialization
func BenchmarkBatchSerialize(b *testing.B) {
	// Create 10 requests
	requests := make([]*internalpb.SearchRequest, 10)
	for i := 0; i < 10; i++ {
		requests[i] = &internalpb.SearchRequest{
			Base: &commonpb.MsgBase{
				MsgID: int64(i),
			},
			CollectionID:     100,
			PlaceholderGroup: make([]byte, 10*768*4), // Smaller for batch test
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BatchSerialize(requests)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBufferPoolGetPut benchmarks buffer pool operations
func BenchmarkBufferPoolGetPut(b *testing.B) {
	pool := NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		buf = append(buf, []byte("test data for benchmarking")...)
		pool.Put(buf)
	}
}
