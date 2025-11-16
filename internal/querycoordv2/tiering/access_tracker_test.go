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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAccessTrackerRecordAccess(t *testing.T) {
	tracker := NewAccessTracker()

	// Record access for segment 1
	tracker.RecordAccess(1, 1024, 10*time.Millisecond)

	// Verify stats
	stats := tracker.GetStats(1)
	assert.Equal(t, int64(1), stats.SegmentID)
	assert.Equal(t, int64(1), stats.AccessCount)
	assert.Equal(t, int64(1024), stats.BytesRead)
	assert.Equal(t, 10*time.Millisecond, stats.AvgLatency)

	// Record another access
	tracker.RecordAccess(1, 2048, 20*time.Millisecond)

	// Verify updated stats
	stats = tracker.GetStats(1)
	assert.Equal(t, int64(2), stats.AccessCount)
	assert.Equal(t, int64(3072), stats.BytesRead)
	assert.True(t, stats.AvgLatency > 10*time.Millisecond)
	assert.True(t, stats.AvgLatency < 20*time.Millisecond)
}

func TestAccessTrackerUpdateTier(t *testing.T) {
	tracker := NewAccessTracker()

	// Update tier for segment 1
	tracker.UpdateTier(1, TierWarm)

	// Verify tier
	stats := tracker.GetStats(1)
	assert.Equal(t, TierWarm, stats.CurrentTier)

	// Update to cold tier
	tracker.UpdateTier(1, TierCold)
	stats = tracker.GetStats(1)
	assert.Equal(t, TierCold, stats.CurrentTier)
}

func TestAccessTrackerGetHotSegments(t *testing.T) {
	tracker := NewAccessTracker()

	// Record accesses
	for i := 0; i < 150; i++ {
		tracker.RecordAccess(1, 1024, 10*time.Millisecond)
	}

	for i := 0; i < 5; i++ {
		tracker.RecordAccess(2, 1024, 10*time.Millisecond)
	}

	// Get hot segments
	hotSegments := tracker.GetHotSegments(100, 1*time.Hour)

	// Only segment 1 should be hot
	assert.Equal(t, 1, len(hotSegments))
	assert.Equal(t, int64(1), hotSegments[0])
}

func TestAccessTrackerGetColdSegments(t *testing.T) {
	tracker := NewAccessTracker()

	// Record access for segment 1 (recent)
	tracker.RecordAccess(1, 1024, 10*time.Millisecond)

	// Record access for segment 2 (old)
	tracker.RecordAccess(2, 1024, 10*time.Millisecond)
	stats := tracker.GetStats(2)
	stats.LastAccess = time.Now().Add(-2 * time.Hour)
	tracker.mu.Lock()
	tracker.segmentAccess[2] = stats
	tracker.mu.Unlock()

	// Get cold segments
	coldSegments := tracker.GetColdSegments(1 * time.Hour)

	// Only segment 2 should be cold
	assert.Equal(t, 1, len(coldSegments))
	assert.Equal(t, int64(2), coldSegments[0])
}

func TestAccessTrackerRemoveSegment(t *testing.T) {
	tracker := NewAccessTracker()

	// Add segment
	tracker.RecordAccess(1, 1024, 10*time.Millisecond)

	// Verify it exists
	allStats := tracker.GetAllStats()
	assert.Equal(t, 1, len(allStats))

	// Remove segment
	tracker.RemoveSegment(1)

	// Verify it's removed
	allStats = tracker.GetAllStats()
	assert.Equal(t, 0, len(allStats))
}

func TestAccessTrackerCleanup(t *testing.T) {
	tracker := NewAccessTracker()

	// Add segments
	tracker.RecordAccess(1, 1024, 10*time.Millisecond)
	tracker.RecordAccess(2, 1024, 10*time.Millisecond)

	// Make segment 2 old
	stats := tracker.GetStats(2)
	stats.LastAccess = time.Now().Add(-2 * time.Hour)
	tracker.mu.Lock()
	tracker.segmentAccess[2] = stats
	tracker.mu.Unlock()

	// Cleanup old segments
	removed := tracker.Cleanup(1 * time.Hour)

	// Verify cleanup
	assert.Equal(t, 1, removed)
	allStats := tracker.GetAllStats()
	assert.Equal(t, 1, len(allStats))
	assert.Contains(t, allStats, int64(1))
}
