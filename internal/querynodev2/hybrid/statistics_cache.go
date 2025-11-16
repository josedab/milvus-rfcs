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
	"sync"
	"time"
)

// StatisticsCache caches collection statistics for selectivity estimation
type StatisticsCache struct {
	mu    sync.RWMutex
	cache map[int64]*cachedStats
}

type cachedStats struct {
	stats      *CollectionStats
	lastUpdate time.Time
}

// NewStatisticsCache creates a new statistics cache
func NewStatisticsCache() *StatisticsCache {
	return &StatisticsCache{
		cache: make(map[int64]*cachedStats),
	}
}

// Get retrieves cached statistics for a collection
func (c *StatisticsCache) Get(collectionID int64) *CollectionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cached, ok := c.cache[collectionID]; ok {
		return cached.stats
	}

	return nil
}

// Update updates cached statistics for a collection
func (c *StatisticsCache) Update(collectionID int64, stats *CollectionStats) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[collectionID] = &cachedStats{
		stats:      stats,
		lastUpdate: time.Now(),
	}
}

// Delete removes cached statistics for a collection
func (c *StatisticsCache) Delete(collectionID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, collectionID)
}

// GetAge returns how long ago the statistics were updated
func (c *StatisticsCache) GetAge(collectionID int64) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cached, ok := c.cache[collectionID]; ok {
		return time.Since(cached.lastUpdate)
	}

	return time.Duration(0)
}

// EvictOld removes statistics older than the specified duration
func (c *StatisticsCache) EvictOld(maxAge time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	now := time.Now()

	for collectionID, cached := range c.cache {
		if now.Sub(cached.lastUpdate) > maxAge {
			delete(c.cache, collectionID)
			evicted++
		}
	}

	return evicted
}

// Clear removes all cached statistics
func (c *StatisticsCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[int64]*cachedStats)
}

// Size returns the number of cached collections
func (c *StatisticsCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.cache)
}
