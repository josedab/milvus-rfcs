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

package hybrid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewStatisticsCache(t *testing.T) {
	cache := NewStatisticsCache()
	assert.NotNil(t, cache)
	assert.NotNil(t, cache.cache)
	assert.Equal(t, 0, cache.Size())
}

func TestStatisticsCache_UpdateAndGet(t *testing.T) {
	cache := NewStatisticsCache()

	stats := &CollectionStats{
		TotalRows: 1000,
		FieldStats: map[string]*FieldStats{
			"field1": {
				Cardinality: 100,
				TotalCount:  1000,
			},
		},
	}

	cache.Update(123, stats)

	retrieved := cache.Get(123)
	assert.NotNil(t, retrieved)
	assert.Equal(t, int64(1000), retrieved.TotalRows)
	assert.Equal(t, 1, len(retrieved.FieldStats))
}

func TestStatisticsCache_GetNotFound(t *testing.T) {
	cache := NewStatisticsCache()

	retrieved := cache.Get(999)
	assert.Nil(t, retrieved)
}

func TestStatisticsCache_Delete(t *testing.T) {
	cache := NewStatisticsCache()

	stats := &CollectionStats{
		TotalRows: 1000,
	}

	cache.Update(123, stats)
	assert.Equal(t, 1, cache.Size())

	cache.Delete(123)
	assert.Equal(t, 0, cache.Size())

	retrieved := cache.Get(123)
	assert.Nil(t, retrieved)
}

func TestStatisticsCache_GetAge(t *testing.T) {
	cache := NewStatisticsCache()

	stats := &CollectionStats{
		TotalRows: 1000,
	}

	cache.Update(123, stats)

	time.Sleep(10 * time.Millisecond)

	age := cache.GetAge(123)
	assert.True(t, age > 0)
	assert.True(t, age >= 10*time.Millisecond)
}

func TestStatisticsCache_GetAge_NotFound(t *testing.T) {
	cache := NewStatisticsCache()

	age := cache.GetAge(999)
	assert.Equal(t, time.Duration(0), age)
}

func TestStatisticsCache_EvictOld(t *testing.T) {
	cache := NewStatisticsCache()

	stats1 := &CollectionStats{TotalRows: 1000}
	stats2 := &CollectionStats{TotalRows: 2000}

	cache.Update(1, stats1)
	time.Sleep(50 * time.Millisecond)
	cache.Update(2, stats2)

	// Evict entries older than 30ms (should evict stats1)
	evicted := cache.EvictOld(30 * time.Millisecond)
	assert.Equal(t, 1, evicted)
	assert.Equal(t, 1, cache.Size())

	// stats1 should be gone
	assert.Nil(t, cache.Get(1))
	// stats2 should still be there
	assert.NotNil(t, cache.Get(2))
}

func TestStatisticsCache_EvictOld_None(t *testing.T) {
	cache := NewStatisticsCache()

	stats := &CollectionStats{TotalRows: 1000}
	cache.Update(1, stats)

	// Evict entries older than 1 hour (should evict nothing)
	evicted := cache.EvictOld(1 * time.Hour)
	assert.Equal(t, 0, evicted)
	assert.Equal(t, 1, cache.Size())
}

func TestStatisticsCache_Clear(t *testing.T) {
	cache := NewStatisticsCache()

	stats1 := &CollectionStats{TotalRows: 1000}
	stats2 := &CollectionStats{TotalRows: 2000}

	cache.Update(1, stats1)
	cache.Update(2, stats2)
	assert.Equal(t, 2, cache.Size())

	cache.Clear()
	assert.Equal(t, 0, cache.Size())
	assert.Nil(t, cache.Get(1))
	assert.Nil(t, cache.Get(2))
}

func TestStatisticsCache_Size(t *testing.T) {
	cache := NewStatisticsCache()
	assert.Equal(t, 0, cache.Size())

	cache.Update(1, &CollectionStats{TotalRows: 1000})
	assert.Equal(t, 1, cache.Size())

	cache.Update(2, &CollectionStats{TotalRows: 2000})
	assert.Equal(t, 2, cache.Size())

	cache.Delete(1)
	assert.Equal(t, 1, cache.Size())
}

func TestStatisticsCache_ConcurrentAccess(t *testing.T) {
	cache := NewStatisticsCache()

	// Test concurrent updates and reads
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			stats := &CollectionStats{TotalRows: int64(i)}
			cache.Update(int64(i%10), stats)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get(int64(i % 10))
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Should not panic and should have some data
	assert.True(t, cache.Size() > 0)
}

func TestStatisticsCache_UpdateOverwrite(t *testing.T) {
	cache := NewStatisticsCache()

	stats1 := &CollectionStats{TotalRows: 1000}
	cache.Update(123, stats1)

	stats2 := &CollectionStats{TotalRows: 2000}
	cache.Update(123, stats2)

	retrieved := cache.Get(123)
	assert.NotNil(t, retrieved)
	assert.Equal(t, int64(2000), retrieved.TotalRows)
	assert.Equal(t, 1, cache.Size())
}
