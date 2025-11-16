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

func TestNewTierManager(t *testing.T) {
	config := &TierManagerConfig{
		Enabled:             true,
		HotTierMaxMemoryGB:  64,
		WarmTierEnabled:     true,
		WarmTierPath:        "/tmp/warm",
		WarmTierMaxSizeGB:   512,
		ColdTierEnabled:     true,
		ColdTierBucket:      "test-bucket",
		ColdTierEndpoint:    "localhost:9000",
		MaxMigrationWorkers: 4,
	}

	tm := NewTierManager(config)
	assert.NotNil(t, tm)
	assert.True(t, tm.enabled)
	assert.NotNil(t, tm.hotTier)
	assert.NotNil(t, tm.warmTier)
	assert.NotNil(t, tm.coldTier)
	assert.NotNil(t, tm.accessTracker)
	assert.NotNil(t, tm.migrator)
}

func TestTierManagerDetermineTier(t *testing.T) {
	config := &TierManagerConfig{
		Enabled:             true,
		HotTierMaxMemoryGB:  64,
		WarmTierEnabled:     true,
		WarmTierPath:        "/tmp/warm",
		WarmTierMaxSizeGB:   512,
		ColdTierEnabled:     true,
		ColdTierBucket:      "test-bucket",
		ColdTierEndpoint:    "localhost:9000",
		MaxMigrationWorkers: 4,
		Policy: &TieringPolicy{
			HotThreshold:            1 * time.Hour,
			WarmThreshold:           24 * time.Hour,
			MinAccessCount:          10,
			HotAccessCountThreshold: 100,
		},
	}

	tm := NewTierManager(config)

	// Segment with high access count and recent access -> Hot
	for i := 0; i < 150; i++ {
		tm.RecordAccess(1, 1024, 10*time.Millisecond)
	}
	tier := tm.DetermineTier(1)
	assert.Equal(t, TierHot, tier)

	// Segment with moderate access -> Warm
	for i := 0; i < 15; i++ {
		tm.RecordAccess(2, 1024, 10*time.Millisecond)
	}
	tier = tm.DetermineTier(2)
	assert.Equal(t, TierWarm, tier)

	// Segment with low access -> Cold
	tm.RecordAccess(3, 1024, 10*time.Millisecond)
	tier = tm.DetermineTier(3)
	assert.Equal(t, TierCold, tier)
}

func TestTierManagerMigrateSegment(t *testing.T) {
	config := &TierManagerConfig{
		Enabled:             true,
		HotTierMaxMemoryGB:  64,
		WarmTierEnabled:     true,
		WarmTierPath:        "/tmp/warm",
		WarmTierMaxSizeGB:   512,
		ColdTierEnabled:     false,
		MaxMigrationWorkers: 4,
	}

	tm := NewTierManager(config)
	err := tm.Start()
	assert.NoError(t, err)
	defer tm.Stop()

	// Record access to put segment in hot tier
	tm.accessTracker.UpdateTier(1, TierHot)

	// Migrate to warm tier
	err = tm.MigrateSegment(1, TierWarm)
	assert.NoError(t, err)

	// Verify migration was scheduled
	status, exists := tm.migrator.GetMigrationStatus(1)
	assert.True(t, exists)
	assert.True(t, status == MigrationStatusPending || status == MigrationStatusRunning)
}

func TestTierManagerGetStatistics(t *testing.T) {
	config := &TierManagerConfig{
		Enabled:             true,
		HotTierMaxMemoryGB:  64,
		WarmTierEnabled:     true,
		WarmTierPath:        "/tmp/warm",
		WarmTierMaxSizeGB:   512,
		ColdTierEnabled:     true,
		ColdTierBucket:      "test-bucket",
		ColdTierEndpoint:    "localhost:9000",
		MaxMigrationWorkers: 4,
	}

	tm := NewTierManager(config)

	stats := tm.GetTierStatistics()
	assert.NotNil(t, stats)
	assert.Contains(t, stats, "hot")
	assert.Contains(t, stats, "warm")
	assert.Contains(t, stats, "cold")
	assert.Contains(t, stats, "migration")
}

func TestTierManagerDisabled(t *testing.T) {
	config := &TierManagerConfig{
		Enabled:             false,
		HotTierMaxMemoryGB:  64,
		MaxMigrationWorkers: 4,
	}

	tm := NewTierManager(config)
	assert.False(t, tm.enabled)

	err := tm.Start()
	assert.NoError(t, err)

	// Recording access should not crash
	tm.RecordAccess(1, 1024, 10*time.Millisecond)

	// Determine tier should return hot
	tier := tm.DetermineTier(1)
	assert.Equal(t, TierHot, tier)

	// Migration should fail
	err = tm.MigrateSegment(1, TierWarm)
	assert.Error(t, err)
}
