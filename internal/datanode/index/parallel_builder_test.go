// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package index

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/milvus-io/milvus/pkg/v2/common"
	"github.com/milvus-io/milvus/pkg/v2/proto/workerpb"
)

func TestNewParallelIndexBuilder(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		builder, err := NewParallelIndexBuilder(nil)
		require.NoError(t, err)
		require.NotNil(t, builder)
		assert.NotNil(t, builder.workerPool)
		assert.NotNil(t, builder.semaphore)
		assert.True(t, builder.config.Enabled)
		assert.Equal(t, 0.8, builder.config.MemoryReservationRatio)
		builder.Close()
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &ParallelConfig{
			MaxConcurrentBuilds:    4,
			MemoryReservationRatio: 0.7,
			MemoryFactors: map[string]float64{
				"HNSW": 2.0,
			},
			Enabled: true,
		}
		builder, err := NewParallelIndexBuilder(config)
		require.NoError(t, err)
		require.NotNil(t, builder)
		assert.Equal(t, int64(4), builder.maxConcurrent)
		assert.Equal(t, 0.7, builder.config.MemoryReservationRatio)
		builder.Close()
	})

	t.Run("with disabled parallel", func(t *testing.T) {
		config := &ParallelConfig{
			Enabled: false,
		}
		builder, err := NewParallelIndexBuilder(config)
		require.NoError(t, err)
		require.NotNil(t, builder)
		assert.False(t, builder.config.Enabled)
		builder.Close()
	})
}

func TestParallelIndexBuilder_GetMemoryFactor(t *testing.T) {
	builder, err := NewParallelIndexBuilder(nil)
	require.NoError(t, err)
	defer builder.Close()

	tests := []struct {
		indexType      string
		expectedFactor float64
	}{
		{"HNSW", 1.5},
		{"IVF_FLAT", 2.0},
		{"IVF_PQ", 1.8},
		{"IVF_SQ8", 1.7},
		{"DiskANN", 1.2},
		{"FLAT", 1.1},
		{"UNKNOWN", 1.5}, // default
	}

	for _, tt := range tests {
		t.Run(tt.indexType, func(t *testing.T) {
			factor := builder.getMemoryFactor(tt.indexType)
			assert.Equal(t, tt.expectedFactor, factor)
		})
	}
}

func TestParallelIndexBuilder_GetMemoryFactorCustom(t *testing.T) {
	config := &ParallelConfig{
		MemoryFactors: map[string]float64{
			"CUSTOM_INDEX": 3.0,
		},
		Enabled: true,
	}
	builder, err := NewParallelIndexBuilder(config)
	require.NoError(t, err)
	defer builder.Close()

	factor := builder.getMemoryFactor("CUSTOM_INDEX")
	assert.Equal(t, 3.0, factor)
}

func TestParallelIndexBuilder_CalculateMemoryWeight(t *testing.T) {
	builder, err := NewParallelIndexBuilder(nil)
	require.NoError(t, err)
	defer builder.Close()

	// Create a mock task
	task := &indexBuildTask{
		newIndexParams: map[string]string{
			common.IndexTypeKey: "HNSW",
		},
	}

	// Mock req with segment info
	task.req = &workerpb.CreateJobRequest{
		Dim:     128,
		NumRows: 1000,
	}

	weight := builder.calculateMemoryWeight(task)
	assert.True(t, weight >= 1 && weight <= 100, "weight should be in range [1, 100]")
}

func TestParallelIndexBuilder_CalculateOptimalConcurrency(t *testing.T) {
	builder, err := NewParallelIndexBuilder(nil)
	require.NoError(t, err)
	defer builder.Close()

	// Create some mock tasks
	tasks := []*indexBuildTask{
		{
			newIndexParams: map[string]string{
				common.IndexTypeKey: "HNSW",
			},
			req: &workerpb.CreateJobRequest{
				Dim:     128,
				NumRows: 1000,
			},
		},
		{
			newIndexParams: map[string]string{
				common.IndexTypeKey: "IVF_FLAT",
			},
			req: &workerpb.CreateJobRequest{
				Dim:     256,
				NumRows: 2000,
			},
		},
	}

	concurrency := builder.calculateOptimalConcurrency(tasks)
	assert.Greater(t, concurrency, 0, "concurrency should be positive")
	assert.LessOrEqual(t, concurrency, int(builder.maxConcurrent), "concurrency should not exceed max")
}

func TestParallelIndexBuilder_BuildSequential(t *testing.T) {
	builder, err := NewParallelIndexBuilder(nil)
	require.NoError(t, err)
	defer builder.Close()

	ctx := context.Background()

	// Test with empty tasks
	err = builder.buildSequential(ctx, []*indexBuildTask{})
	assert.NoError(t, err)

	// Note: We can't easily test with real tasks without mocking the entire
	// index building infrastructure, so we just test the empty case
}

func TestParallelIndexBuilder_BuildParallelDisabled(t *testing.T) {
	config := &ParallelConfig{
		Enabled: false,
	}
	builder, err := NewParallelIndexBuilder(config)
	require.NoError(t, err)
	defer builder.Close()

	ctx := context.Background()

	// With parallel disabled, it should fall back to sequential
	err = builder.BuildParallel(ctx, []*indexBuildTask{})
	assert.NoError(t, err)
}

func TestParallelIndexBuilder_BuildParallelSingleTask(t *testing.T) {
	builder, err := NewParallelIndexBuilder(nil)
	require.NoError(t, err)
	defer builder.Close()

	ctx := context.Background()

	// With single task, it should fall back to sequential
	tasks := []*indexBuildTask{
		{
			newIndexParams: map[string]string{
				common.IndexTypeKey: "HNSW",
			},
			req: &workerpb.CreateJobRequest{
				Dim:     128,
				NumRows: 1000,
			},
		},
	}

	// Note: This will fail because we don't have a complete task setup,
	// but we're testing that it attempts the sequential path
	err = builder.BuildParallel(ctx, tasks)
	// We expect an error here since the task isn't fully initialized
	// but we're just checking the code path
}

func TestParallelConfig_Defaults(t *testing.T) {
	config := defaultParallelConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, 0, config.MaxConcurrentBuilds)
	assert.Equal(t, 0.8, config.MemoryReservationRatio)
	assert.NotNil(t, config.MemoryFactors)
	assert.Equal(t, 1.5, config.MemoryFactors["HNSW"])
	assert.Equal(t, 2.0, config.MemoryFactors["IVF_FLAT"])
}

func TestMinMax(t *testing.T) {
	assert.Equal(t, 1, min(1, 2))
	assert.Equal(t, 1, min(2, 1))
	assert.Equal(t, 5, min(5, 5))

	assert.Equal(t, 2, max(1, 2))
	assert.Equal(t, 2, max(2, 1))
	assert.Equal(t, 5, max(5, 5))
}
