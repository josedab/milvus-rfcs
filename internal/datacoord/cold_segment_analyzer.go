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

package datacoord

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/util/paramtable"
)

// ColdSegmentAnalyzer tracks segment access patterns to identify
// segments that haven't been accessed recently (cold segments)
type ColdSegmentAnalyzer struct {
	mu sync.RWMutex

	// Track last access time per segment
	segmentAccessTimes map[int64]time.Time
}

// NewColdSegmentAnalyzer creates a new cold segment analyzer
func NewColdSegmentAnalyzer() *ColdSegmentAnalyzer {
	return &ColdSegmentAnalyzer{
		segmentAccessTimes: make(map[int64]time.Time),
	}
}

// RecordAccess updates the last access time for a segment
func (a *ColdSegmentAnalyzer) RecordAccess(segmentID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.segmentAccessTimes[segmentID] = time.Now()

	log.Debug("Segment access recorded",
		zap.Int64("segmentID", segmentID),
		zap.Time("accessTime", time.Now()))
}

// IdentifyColdSegments returns segments not accessed recently
func (a *ColdSegmentAnalyzer) IdentifyColdSegments() []int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	coldThreshold := a.getColdThreshold()
	thresholdTime := time.Now().Add(-coldThreshold)
	cold := []int64{}

	for segmentID, lastAccess := range a.segmentAccessTimes {
		if lastAccess.Before(thresholdTime) {
			cold = append(cold, segmentID)
		}
	}

	log.Debug("Identified cold segments",
		zap.Int("count", len(cold)),
		zap.Duration("threshold", coldThreshold))

	return cold
}

// IsSegmentCold checks if a specific segment is cold
func (a *ColdSegmentAnalyzer) IsSegmentCold(segmentID int64) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	lastAccess, exists := a.segmentAccessTimes[segmentID]
	if !exists {
		// If we've never seen this segment, consider it cold
		return true
	}

	coldThreshold := a.getColdThreshold()
	thresholdTime := time.Now().Add(-coldThreshold)

	return lastAccess.Before(thresholdTime)
}

// getColdThreshold returns the configured cold segment threshold
func (a *ColdSegmentAnalyzer) getColdThreshold() time.Duration {
	params := paramtable.Get()
	coldHours := params.DataCoordCfg.SmartCompactionColdSegmentHours.GetAsInt()
	return time.Duration(coldHours) * time.Hour
}

// CleanupOldEntries removes tracking data for very old segments
// This should be called periodically to prevent unbounded memory growth
func (a *ColdSegmentAnalyzer) CleanupOldEntries(maxAge time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoffTime := time.Now().Add(-maxAge)
	removed := 0

	for segmentID, lastAccess := range a.segmentAccessTimes {
		if lastAccess.Before(cutoffTime) {
			delete(a.segmentAccessTimes, segmentID)
			removed++
		}
	}

	if removed > 0 {
		log.Info("Cleaned up old segment access entries",
			zap.Int("removed", removed),
			zap.Duration("maxAge", maxAge))
	}
}

// GetSegmentCount returns the number of segments being tracked
func (a *ColdSegmentAnalyzer) GetSegmentCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return len(a.segmentAccessTimes)
}

// GetLastAccessTime returns the last access time for a segment
func (a *ColdSegmentAnalyzer) GetLastAccessTime(segmentID int64) (time.Time, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	lastAccess, exists := a.segmentAccessTimes[segmentID]
	return lastAccess, exists
}

// RemoveSegment removes tracking for a specific segment
// This should be called when a segment is dropped or compacted
func (a *ColdSegmentAnalyzer) RemoveSegment(segmentID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.segmentAccessTimes, segmentID)

	log.Debug("Removed segment from tracking",
		zap.Int64("segmentID", segmentID))
}
