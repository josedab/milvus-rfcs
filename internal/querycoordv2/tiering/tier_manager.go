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
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
)

// TierManager manages segment tiering and migration
type TierManager struct {
	mu            sync.RWMutex
	hotTier       Tier
	warmTier      Tier
	coldTier      Tier
	accessTracker *AccessTracker
	migrator      *TierMigrator
	policy        *TieringPolicy
	enabled       bool
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// TierManagerConfig holds configuration for TierManager
type TierManagerConfig struct {
	Enabled             bool
	HotTierMaxMemoryGB  int64
	WarmTierEnabled     bool
	WarmTierPath        string
	WarmTierMaxSizeGB   int64
	ColdTierEnabled     bool
	ColdTierBucket      string
	ColdTierEndpoint    string
	Policy              *TieringPolicy
	MaxMigrationWorkers int
}

// NewTierManager creates a new TierManager
func NewTierManager(config *TierManagerConfig) *TierManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize tiers
	hotTier := NewMemoryTier(config.HotTierMaxMemoryGB)

	var warmTier Tier
	if config.WarmTierEnabled {
		warmTier = NewSSDTier(config.WarmTierPath, config.WarmTierMaxSizeGB)
	}

	var coldTier Tier
	if config.ColdTierEnabled {
		coldTier = NewObjectStorageTier(config.ColdTierBucket, config.ColdTierEndpoint, 10000) // 10TB default for cold tier
	}

	// Build tier map for migrator
	tiers := make(map[StorageTier]Tier)
	tiers[TierHot] = hotTier
	if warmTier != nil {
		tiers[TierWarm] = warmTier
	}
	if coldTier != nil {
		tiers[TierCold] = coldTier
	}

	maxWorkers := config.MaxMigrationWorkers
	if maxWorkers <= 0 {
		maxWorkers = 4 // Default to 4 concurrent migrations
	}

	policy := config.Policy
	if policy == nil {
		policy = DefaultTieringPolicy()
	}

	return &TierManager{
		hotTier:       hotTier,
		warmTier:      warmTier,
		coldTier:      coldTier,
		accessTracker: NewAccessTracker(),
		migrator:      NewTierMigrator(tiers, maxWorkers),
		policy:        policy,
		enabled:       config.Enabled,
		logger:        log.L(),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start starts the tier manager
func (tm *TierManager) Start() error {
	if !tm.enabled {
		tm.logger.Info("tiered storage is disabled")
		return nil
	}

	tm.migrator.Start()

	// Start background tier optimization
	tm.wg.Add(1)
	go tm.tierOptimizationWorker()

	tm.logger.Info("tier manager started",
		zap.Int64("hotCapacityGB", tm.hotTier.GetCapacity()/(1024*1024*1024)))

	return nil
}

// Stop stops the tier manager
func (tm *TierManager) Stop() {
	if !tm.enabled {
		return
	}

	tm.cancel()
	tm.wg.Wait()
	tm.migrator.Stop()

	tm.logger.Info("tier manager stopped")
}

// RecordAccess records an access to a segment
func (tm *TierManager) RecordAccess(segmentID int64, bytesRead int64, latency time.Duration) {
	if !tm.enabled {
		return
	}

	tm.accessTracker.RecordAccess(segmentID, bytesRead, latency)
}

// DetermineTier determines the optimal tier for a segment based on access patterns
func (tm *TierManager) DetermineTier(segmentID int64) StorageTier {
	if !tm.enabled {
		return TierHot // Default to hot if tiering is disabled
	}

	stats := tm.accessTracker.GetStats(segmentID)

	// Hot: accessed recently AND frequently
	if time.Since(stats.LastAccess) < tm.policy.HotThreshold &&
		stats.AccessCount >= tm.policy.HotAccessCountThreshold {
		return TierHot
	}

	// Warm: accessed in last 24h OR moderate access count
	if time.Since(stats.LastAccess) < tm.policy.WarmThreshold ||
		stats.AccessCount >= tm.policy.MinAccessCount {
		if tm.warmTier != nil {
			return TierWarm
		}
		return TierHot // Fall back to hot if warm tier is not available
	}

	// Cold: everything else
	if tm.coldTier != nil {
		return TierCold
	}

	// Fall back to warm or hot if cold tier is not available
	if tm.warmTier != nil {
		return TierWarm
	}
	return TierHot
}

// MigrateSegment schedules a migration for a segment to a target tier
func (tm *TierManager) MigrateSegment(segmentID int64, targetTier StorageTier) error {
	if !tm.enabled {
		return fmt.Errorf("tiered storage is disabled")
	}

	stats := tm.accessTracker.GetStats(segmentID)
	currentTier := stats.CurrentTier

	if currentTier == targetTier {
		return nil // Already in the target tier
	}

	// Determine priority based on migration direction
	priority := tm.getMigrationPriority(currentTier, targetTier)

	err := tm.migrator.ScheduleMigration(segmentID, currentTier, targetTier, priority)
	if err != nil {
		return fmt.Errorf("failed to schedule migration: %w", err)
	}

	// Update tier in access tracker (will be updated again after migration completes)
	tm.accessTracker.UpdateTier(segmentID, targetTier)

	return nil
}

// getMigrationPriority determines migration priority based on tiers
func (tm *TierManager) getMigrationPriority(from, to StorageTier) int {
	// Promotions (to hotter tier) have higher priority than demotions
	if to < from {
		// Promotion
		return 100 - int(to)*10
	}
	// Demotion
	return 50 - int(from)*10
}

// tierOptimizationWorker periodically optimizes tier placement
func (tm *TierManager) tierOptimizationWorker() {
	defer tm.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.optimizeTiers()
		}
	}
}

// optimizeTiers analyzes all segments and schedules migrations
func (tm *TierManager) optimizeTiers() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	allStats := tm.accessTracker.GetAllStats()
	tm.logger.Info("optimizing tiers", zap.Int("segmentCount", len(allStats)))

	promotions := 0
	demotions := 0

	for segmentID, stats := range allStats {
		targetTier := tm.DetermineTier(segmentID)

		if targetTier != stats.CurrentTier {
			// Check if migration is not already scheduled
			if status, exists := tm.migrator.GetMigrationStatus(segmentID); exists {
				if status == MigrationStatusPending || status == MigrationStatusRunning {
					continue // Skip if migration is already in progress
				}
			}

			// Check tier capacity before scheduling
			if !tm.checkTierCapacity(targetTier, segmentID) {
				continue // Skip if target tier is full
			}

			priority := tm.getMigrationPriority(stats.CurrentTier, targetTier)
			err := tm.migrator.ScheduleMigration(segmentID, stats.CurrentTier, targetTier, priority)
			if err != nil {
				tm.logger.Warn("failed to schedule migration during optimization",
					zap.Error(err),
					zap.Int64("segmentID", segmentID))
				continue
			}

			if targetTier < stats.CurrentTier {
				promotions++
			} else {
				demotions++
			}
		}
	}

	if promotions > 0 || demotions > 0 {
		tm.logger.Info("tier optimization scheduled migrations",
			zap.Int("promotions", promotions),
			zap.Int("demotions", demotions))
	}
}

// checkTierCapacity checks if a tier has enough capacity for a segment
func (tm *TierManager) checkTierCapacity(tier StorageTier, segmentID int64) bool {
	var targetTier Tier
	switch tier {
	case TierHot:
		targetTier = tm.hotTier
	case TierWarm:
		targetTier = tm.warmTier
	case TierCold:
		targetTier = tm.coldTier
	default:
		return false
	}

	if targetTier == nil {
		return false
	}

	// For simplicity, we assume segment size is tracked in access tracker
	// In a real implementation, this would query the actual segment metadata
	availableSpace := targetTier.GetAvailableSpace()

	// Require at least 10% free space
	threshold := targetTier.GetCapacity() / 10
	return availableSpace > threshold
}

// GetTierStatistics returns statistics about all tiers
func (tm *TierManager) GetTierStatistics() map[string]interface{} {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stats := make(map[string]interface{})

	if tm.hotTier != nil {
		stats["hot"] = map[string]interface{}{
			"capacity":  tm.hotTier.GetCapacity(),
			"used":      tm.hotTier.GetUsedSpace(),
			"available": tm.hotTier.GetAvailableSpace(),
		}
	}

	if tm.warmTier != nil {
		stats["warm"] = map[string]interface{}{
			"capacity":  tm.warmTier.GetCapacity(),
			"used":      tm.warmTier.GetUsedSpace(),
			"available": tm.warmTier.GetAvailableSpace(),
		}
	}

	if tm.coldTier != nil {
		stats["cold"] = map[string]interface{}{
			"capacity":  tm.coldTier.GetCapacity(),
			"used":      tm.coldTier.GetUsedSpace(),
			"available": tm.coldTier.GetAvailableSpace(),
		}
	}

	stats["migration"] = tm.migrator.GetStatistics()

	return stats
}

// GetSegmentTier returns the current tier of a segment
func (tm *TierManager) GetSegmentTier(segmentID int64) StorageTier {
	stats := tm.accessTracker.GetStats(segmentID)
	return stats.CurrentTier
}

// IsEnabled returns whether tiered storage is enabled
func (tm *TierManager) IsEnabled() bool {
	return tm.enabled
}
