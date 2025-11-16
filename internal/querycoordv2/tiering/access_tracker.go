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
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
)

// AccessTracker tracks access patterns for segments
type AccessTracker struct {
	mu            sync.RWMutex
	segmentAccess map[int64]*AccessStats
	logger        *zap.Logger
}

// NewAccessTracker creates a new AccessTracker
func NewAccessTracker() *AccessTracker {
	return &AccessTracker{
		segmentAccess: make(map[int64]*AccessStats),
		logger:        log.L(),
	}
}

// RecordAccess records an access to a segment
func (at *AccessTracker) RecordAccess(segmentID int64, bytesRead int64, latency time.Duration) {
	at.mu.Lock()
	defer at.mu.Unlock()

	stats, exists := at.segmentAccess[segmentID]
	if !exists {
		stats = &AccessStats{
			SegmentID:   segmentID,
			LastAccess:  time.Now(),
			AccessCount: 0,
			BytesRead:   0,
			AvgLatency:  0,
			CurrentTier: TierHot, // Default to hot tier
		}
		at.segmentAccess[segmentID] = stats
	}

	// Update access statistics
	stats.LastAccess = time.Now()
	stats.AccessCount++
	stats.BytesRead += bytesRead

	// Update average latency using exponential moving average
	if stats.AvgLatency == 0 {
		stats.AvgLatency = latency
	} else {
		// EMA with alpha = 0.3
		stats.AvgLatency = time.Duration(0.7*float64(stats.AvgLatency) + 0.3*float64(latency))
	}
}

// GetStats returns access statistics for a segment
func (at *AccessTracker) GetStats(segmentID int64) *AccessStats {
	at.mu.RLock()
	defer at.mu.RUnlock()

	stats, exists := at.segmentAccess[segmentID]
	if !exists {
		// Return default stats for unknown segment
		return &AccessStats{
			SegmentID:   segmentID,
			LastAccess:  time.Now(),
			AccessCount: 0,
			BytesRead:   0,
			AvgLatency:  0,
			CurrentTier: TierCold, // Unknown segments default to cold
		}
	}

	// Return a copy to avoid race conditions
	statsCopy := *stats
	return &statsCopy
}

// UpdateTier updates the current tier for a segment
func (at *AccessTracker) UpdateTier(segmentID int64, tier StorageTier) {
	at.mu.Lock()
	defer at.mu.Unlock()

	stats, exists := at.segmentAccess[segmentID]
	if !exists {
		stats = &AccessStats{
			SegmentID:   segmentID,
			LastAccess:  time.Now(),
			AccessCount: 0,
			CurrentTier: tier,
		}
		at.segmentAccess[segmentID] = stats
	}

	oldTier := stats.CurrentTier
	stats.CurrentTier = tier
	stats.LastMigration = time.Now()

	at.logger.Info("segment tier updated",
		zap.Int64("segmentID", segmentID),
		zap.String("oldTier", oldTier.String()),
		zap.String("newTier", tier.String()))
}

// GetAllStats returns access statistics for all segments
func (at *AccessTracker) GetAllStats() map[int64]*AccessStats {
	at.mu.RLock()
	defer at.mu.RUnlock()

	result := make(map[int64]*AccessStats, len(at.segmentAccess))
	for segmentID, stats := range at.segmentAccess {
		statsCopy := *stats
		result[segmentID] = &statsCopy
	}

	return result
}

// RemoveSegment removes tracking for a segment
func (at *AccessTracker) RemoveSegment(segmentID int64) {
	at.mu.Lock()
	defer at.mu.Unlock()

	delete(at.segmentAccess, segmentID)
	at.logger.Debug("segment removed from access tracker",
		zap.Int64("segmentID", segmentID))
}

// GetHotSegments returns segments that are frequently accessed
func (at *AccessTracker) GetHotSegments(minAccessCount int64, maxAge time.Duration) []int64 {
	at.mu.RLock()
	defer at.mu.RUnlock()

	now := time.Now()
	hotSegments := make([]int64, 0)

	for segmentID, stats := range at.segmentAccess {
		if stats.AccessCount >= minAccessCount && now.Sub(stats.LastAccess) < maxAge {
			hotSegments = append(hotSegments, segmentID)
		}
	}

	return hotSegments
}

// GetColdSegments returns segments that haven't been accessed recently
func (at *AccessTracker) GetColdSegments(maxAge time.Duration) []int64 {
	at.mu.RLock()
	defer at.mu.RUnlock()

	now := time.Now()
	coldSegments := make([]int64, 0)

	for segmentID, stats := range at.segmentAccess {
		if now.Sub(stats.LastAccess) > maxAge {
			coldSegments = append(coldSegments, segmentID)
		}
	}

	return coldSegments
}

// Cleanup removes statistics for segments older than the specified duration
func (at *AccessTracker) Cleanup(maxAge time.Duration) int {
	at.mu.Lock()
	defer at.mu.Unlock()

	now := time.Now()
	removed := 0

	for segmentID, stats := range at.segmentAccess {
		if now.Sub(stats.LastAccess) > maxAge {
			delete(at.segmentAccess, segmentID)
			removed++
		}
	}

	if removed > 0 {
		at.logger.Info("cleaned up old segment statistics",
			zap.Int("removedCount", removed))
	}

	return removed
}
