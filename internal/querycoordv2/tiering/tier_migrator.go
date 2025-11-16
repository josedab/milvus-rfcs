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
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
)

// TierMigrator handles migration of segments between storage tiers
type TierMigrator struct {
	mu             sync.RWMutex
	tiers          map[StorageTier]Tier
	pendingTasks   []*MigrationTask
	runningTasks   map[int64]*MigrationTask
	completedTasks []*MigrationTask
	logger         *zap.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	maxConcurrent  int
}

// NewTierMigrator creates a new tier migrator
func NewTierMigrator(tiers map[StorageTier]Tier, maxConcurrent int) *TierMigrator {
	ctx, cancel := context.WithCancel(context.Background())
	return &TierMigrator{
		tiers:          tiers,
		pendingTasks:   make([]*MigrationTask, 0),
		runningTasks:   make(map[int64]*MigrationTask),
		completedTasks: make([]*MigrationTask, 0),
		logger:         log.L(),
		ctx:            ctx,
		cancel:         cancel,
		maxConcurrent:  maxConcurrent,
	}
}

// Start starts the migration worker
func (tm *TierMigrator) Start() {
	tm.wg.Add(1)
	go tm.migrationWorker()
	tm.logger.Info("tier migrator started")
}

// Stop stops the migration worker
func (tm *TierMigrator) Stop() {
	tm.cancel()
	tm.wg.Wait()
	tm.logger.Info("tier migrator stopped")
}

// ScheduleMigration schedules a migration task
func (tm *TierMigrator) ScheduleMigration(segmentID int64, fromTier, toTier StorageTier, priority int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if already running
	if _, exists := tm.runningTasks[segmentID]; exists {
		return fmt.Errorf("migration already in progress for segment %d", segmentID)
	}

	// Check if already pending
	for _, task := range tm.pendingTasks {
		if task.SegmentID == segmentID {
			return fmt.Errorf("migration already pending for segment %d", segmentID)
		}
	}

	task := &MigrationTask{
		SegmentID:  segmentID,
		FromTier:   fromTier,
		ToTier:     toTier,
		Priority:   priority,
		CreateTime: time.Now(),
		Status:     MigrationStatusPending,
	}

	tm.pendingTasks = append(tm.pendingTasks, task)

	// Sort by priority (higher priority first)
	sort.Slice(tm.pendingTasks, func(i, j int) bool {
		return tm.pendingTasks[i].Priority > tm.pendingTasks[j].Priority
	})

	tm.logger.Info("migration scheduled",
		zap.Int64("segmentID", segmentID),
		zap.String("fromTier", fromTier.String()),
		zap.String("toTier", toTier.String()),
		zap.Int("priority", priority))

	return nil
}

// migrationWorker processes migration tasks
func (tm *TierMigrator) migrationWorker() {
	defer tm.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.processPendingTasks()
		}
	}
}

// processPendingTasks processes pending migration tasks
func (tm *TierMigrator) processPendingTasks() {
	tm.mu.Lock()

	// Check how many tasks can be started
	availableSlots := tm.maxConcurrent - len(tm.runningTasks)
	if availableSlots <= 0 || len(tm.pendingTasks) == 0 {
		tm.mu.Unlock()
		return
	}

	// Start tasks up to the available slots
	tasksToStart := make([]*MigrationTask, 0)
	for i := 0; i < availableSlots && i < len(tm.pendingTasks); i++ {
		task := tm.pendingTasks[i]
		tasksToStart = append(tasksToStart, task)
		tm.runningTasks[task.SegmentID] = task
		task.Status = MigrationStatusRunning
	}

	// Remove started tasks from pending
	if len(tasksToStart) > 0 {
		tm.pendingTasks = tm.pendingTasks[len(tasksToStart):]
	}

	tm.mu.Unlock()

	// Execute tasks
	for _, task := range tasksToStart {
		tm.wg.Add(1)
		go tm.executeMigration(task)
	}
}

// executeMigration executes a migration task
func (tm *TierMigrator) executeMigration(task *MigrationTask) {
	defer tm.wg.Done()

	tm.logger.Info("starting migration",
		zap.Int64("segmentID", task.SegmentID),
		zap.String("fromTier", task.FromTier.String()),
		zap.String("toTier", task.ToTier.String()))

	// Get source and destination tiers
	sourceTier, sourceExists := tm.tiers[task.FromTier]
	destTier, destExists := tm.tiers[task.ToTier]

	if !sourceExists || !destTier {
		tm.finishMigration(task, MigrationStatusFailed)
		tm.logger.Error("tier not found",
			zap.String("fromTier", task.FromTier.String()),
			zap.String("toTier", task.ToTier.String()))
		return
	}

	// Simulate data transfer
	// In a real implementation, this would:
	// 1. Read data from source tier
	// 2. Write data to destination tier
	// 3. Verify the transfer
	// 4. Remove from source tier

	ctx, cancel := context.WithTimeout(tm.ctx, 5*time.Minute)
	defer cancel()

	// Check if segment exists in source tier
	if !sourceTier.HasSegment(task.SegmentID) {
		tm.finishMigration(task, MigrationStatusFailed)
		tm.logger.Error("segment not found in source tier",
			zap.Int64("segmentID", task.SegmentID),
			zap.String("sourceTier", task.FromTier.String()))
		return
	}

	// Get segment size
	size, err := sourceTier.GetSegmentSize(task.SegmentID)
	if err != nil {
		tm.finishMigration(task, MigrationStatusFailed)
		tm.logger.Error("failed to get segment size",
			zap.Error(err),
			zap.Int64("segmentID", task.SegmentID))
		return
	}

	// Simulate data transfer with a small delay
	time.Sleep(100 * time.Millisecond)

	// For simulation, we'll just update the metadata
	// In real implementation, we would transfer actual data
	dummyData := make([]byte, size)

	// Load to destination tier
	if err := destTier.LoadSegment(ctx, task.SegmentID, dummyData); err != nil {
		tm.finishMigration(task, MigrationStatusFailed)
		tm.logger.Error("failed to load segment to destination tier",
			zap.Error(err),
			zap.Int64("segmentID", task.SegmentID),
			zap.String("destTier", task.ToTier.String()))
		return
	}

	// Unload from source tier
	if err := sourceTier.UnloadSegment(ctx, task.SegmentID); err != nil {
		tm.logger.Warn("failed to unload segment from source tier",
			zap.Error(err),
			zap.Int64("segmentID", task.SegmentID),
			zap.String("sourceTier", task.FromTier.String()))
		// Don't fail the migration if unload fails
	}

	tm.finishMigration(task, MigrationStatusCompleted)
	tm.logger.Info("migration completed",
		zap.Int64("segmentID", task.SegmentID),
		zap.String("fromTier", task.FromTier.String()),
		zap.String("toTier", task.ToTier.String()),
		zap.Int64("sizeBytes", size))
}

// finishMigration marks a migration task as finished
func (tm *TierMigrator) finishMigration(task *MigrationTask, status MigrationStatus) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task.Status = status
	delete(tm.runningTasks, task.SegmentID)
	tm.completedTasks = append(tm.completedTasks, task)

	// Keep only last 1000 completed tasks
	if len(tm.completedTasks) > 1000 {
		tm.completedTasks = tm.completedTasks[len(tm.completedTasks)-1000:]
	}
}

// GetMigrationStatus returns the status of a migration task
func (tm *TierMigrator) GetMigrationStatus(segmentID int64) (MigrationStatus, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if task, exists := tm.runningTasks[segmentID]; exists {
		return task.Status, true
	}

	for _, task := range tm.pendingTasks {
		if task.SegmentID == segmentID {
			return task.Status, true
		}
	}

	for i := len(tm.completedTasks) - 1; i >= 0; i-- {
		if tm.completedTasks[i].SegmentID == segmentID {
			return tm.completedTasks[i].Status, true
		}
	}

	return MigrationStatusPending, false
}

// GetStatistics returns migration statistics
func (tm *TierMigrator) GetStatistics() map[string]int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	completed := 0
	failed := 0

	for _, task := range tm.completedTasks {
		if task.Status == MigrationStatusCompleted {
			completed++
		} else if task.Status == MigrationStatusFailed {
			failed++
		}
	}

	return map[string]int{
		"pending":   len(tm.pendingTasks),
		"running":   len(tm.runningTasks),
		"completed": completed,
		"failed":    failed,
	}
}
