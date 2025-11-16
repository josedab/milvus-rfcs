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

package tiering

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemoryTier(t *testing.T) {
	tier := NewMemoryTier(1) // 1GB capacity
	assert.Equal(t, TierHot, tier.GetTierType())

	ctx := context.Background()

	// Load segment
	data := make([]byte, 1024*1024) // 1MB
	err := tier.LoadSegment(ctx, 1, data)
	assert.NoError(t, err)

	// Check segment exists
	assert.True(t, tier.HasSegment(1))

	// Get segment size
	size, err := tier.GetSegmentSize(1)
	assert.NoError(t, err)
	assert.Equal(t, int64(1024*1024), size)

	// Check capacity
	assert.Equal(t, int64(1024*1024*1024), tier.GetCapacity())
	assert.Equal(t, int64(1024*1024), tier.GetUsedSpace())

	// Unload segment
	err = tier.UnloadSegment(ctx, 1)
	assert.NoError(t, err)
	assert.False(t, tier.HasSegment(1))
	assert.Equal(t, int64(0), tier.GetUsedSpace())
}

func TestMemoryTierCapacityExceeded(t *testing.T) {
	tier := NewMemoryTier(1) // 1GB capacity

	ctx := context.Background()

	// Try to load data larger than capacity
	data := make([]byte, 2*1024*1024*1024) // 2GB
	err := tier.LoadSegment(ctx, 1, data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enough space")
}

func TestSSDTier(t *testing.T) {
	tier := NewSSDTier("/tmp/ssd", 10) // 10GB capacity
	assert.Equal(t, TierWarm, tier.GetTierType())

	ctx := context.Background()

	// Load segment
	data := make([]byte, 1024*1024) // 1MB
	err := tier.LoadSegment(ctx, 1, data)
	assert.NoError(t, err)

	// Check segment exists
	assert.True(t, tier.HasSegment(1))

	// Get segment size
	size, err := tier.GetSegmentSize(1)
	assert.NoError(t, err)
	assert.Equal(t, int64(1024*1024), size)

	// Unload segment
	err = tier.UnloadSegment(ctx, 1)
	assert.NoError(t, err)
	assert.False(t, tier.HasSegment(1))
}

func TestObjectStorageTier(t *testing.T) {
	tier := NewObjectStorageTier("test-bucket", "localhost:9000", 100) // 100GB capacity
	assert.Equal(t, TierCold, tier.GetTierType())

	ctx := context.Background()

	// Load segment
	data := make([]byte, 1024*1024) // 1MB
	err := tier.LoadSegment(ctx, 1, data)
	assert.NoError(t, err)

	// Check segment exists
	assert.True(t, tier.HasSegment(1))

	// Get segment size
	size, err := tier.GetSegmentSize(1)
	assert.NoError(t, err)
	assert.Equal(t, int64(1024*1024), size)

	// Unload segment
	err = tier.UnloadSegment(ctx, 1)
	assert.NoError(t, err)
	assert.False(t, tier.HasSegment(1))
}

func TestTierUnloadNonExistent(t *testing.T) {
	tier := NewMemoryTier(1)
	ctx := context.Background()

	// Try to unload non-existent segment
	err := tier.UnloadSegment(ctx, 999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTierGetSizeNonExistent(t *testing.T) {
	tier := NewMemoryTier(1)

	// Try to get size of non-existent segment
	_, err := tier.GetSegmentSize(999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
