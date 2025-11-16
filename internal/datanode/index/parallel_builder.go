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
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"github.com/milvus-io/milvus/pkg/v2/common"
	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/util/hardware"
)

// ParallelIndexBuilder builds multiple indexes concurrently with memory awareness
type ParallelIndexBuilder struct {
	workerPool    *ants.Pool
	semaphore     *semaphore.Weighted
	maxConcurrent int64
	config        *ParallelConfig
}

// ParallelConfig defines configuration for parallel index building
type ParallelConfig struct {
	// Maximum concurrent builds (0 = auto-detect)
	MaxConcurrentBuilds int

	// Memory reservation (0.0-1.0, default 0.8 = 80%)
	MemoryReservationRatio float64

	// Index type memory factors
	MemoryFactors map[string]float64 // indexType -> multiplier

	// Enable/disable parallelization
	Enabled bool
}

// NewParallelIndexBuilder creates a new parallel index builder
func NewParallelIndexBuilder(config *ParallelConfig) (*ParallelIndexBuilder, error) {
	if config == nil {
		config = defaultParallelConfig()
	}

	// Create worker pool
	poolSize := config.MaxConcurrentBuilds
	if poolSize == 0 {
		poolSize = runtime.NumCPU()
	}

	pool, err := ants.NewPool(poolSize, ants.WithPreAlloc(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create worker pool: %w", err)
	}

	// Create semaphore for memory control
	// Weight represents memory slots, acquired based on task size
	sem := semaphore.NewWeighted(100) // 100 memory units

	return &ParallelIndexBuilder{
		workerPool:    pool,
		semaphore:     sem,
		maxConcurrent: int64(poolSize),
		config:        config,
	}, nil
}

// BuildParallel executes multiple index build tasks concurrently
func (b *ParallelIndexBuilder) BuildParallel(
	ctx context.Context,
	tasks []*indexBuildTask,
) error {
	if !b.config.Enabled || len(tasks) <= 1 {
		// Fallback to sequential for single task or disabled
		return b.buildSequential(ctx, tasks)
	}

	log.Info("Starting parallel index build",
		zap.Int("numTasks", len(tasks)),
		zap.Int64("maxConcurrent", b.maxConcurrent))

	// Calculate optimal concurrency based on available resources
	concurrency := b.calculateOptimalConcurrency(tasks)

	log.Info("Calculated optimal concurrency",
		zap.Int("concurrency", concurrency),
		zap.Int("cpuCores", runtime.NumCPU()))

	// Error handling
	errChan := make(chan error, len(tasks))
	var wg sync.WaitGroup

	// Submit tasks to worker pool
	for i, task := range tasks {
		wg.Add(1)

		taskIndex := i
		taskCopy := task

		// Calculate memory weight for this task
		memoryWeight := b.calculateMemoryWeight(taskCopy)

		// Submit to pool
		err := b.workerPool.Submit(func() {
			defer wg.Done()

			// Acquire memory semaphore
			if err := b.semaphore.Acquire(ctx, memoryWeight); err != nil {
				errChan <- fmt.Errorf("task %d: semaphore acquire failed: %w",
					taskIndex, err)
				return
			}
			defer b.semaphore.Release(memoryWeight)

			// Execute index build
			segmentID := taskCopy.req.GetSegmentID()
			indexType := taskCopy.newIndexParams[common.IndexTypeKey]

			log.Info("Starting index build",
				zap.Int("taskIndex", taskIndex),
				zap.Int64("segmentID", segmentID),
				zap.String("indexType", indexType),
				zap.Int64("memoryWeight", memoryWeight))

			startTime := time.Now()

			if err := taskCopy.Execute(ctx); err != nil {
				errChan <- fmt.Errorf("task %d (segment %d): build failed: %w",
					taskIndex, segmentID, err)
				return
			}

			duration := time.Since(startTime)
			log.Info("Index build completed",
				zap.Int("taskIndex", taskIndex),
				zap.Int64("segmentID", segmentID),
				zap.Duration("duration", duration))
		})

		if err != nil {
			wg.Done()
			errChan <- fmt.Errorf("task %d: submit failed: %w", taskIndex, err)
		}
	}

	// Wait for all tasks
	wg.Wait()
	close(errChan)

	// Check for errors
	if len(errChan) > 0 {
		// Collect all errors
		var errors []error
		for err := range errChan {
			errors = append(errors, err)
		}

		// Return first error (could be improved to return all)
		return errors[0]
	}

	log.Info("Parallel index build completed successfully",
		zap.Int("numTasks", len(tasks)))

	return nil
}

// calculateOptimalConcurrency determines how many tasks can run in parallel
func (b *ParallelIndexBuilder) calculateOptimalConcurrency(
	tasks []*indexBuildTask,
) int {
	// Factor 1: CPU cores available
	maxByCPU := runtime.NumCPU()

	// Factor 2: Memory constraints
	totalMemory := hardware.GetMemoryCount()
	availableMemory := float64(totalMemory) * b.config.MemoryReservationRatio

	// Estimate average memory per task
	avgSegmentSize := b.calculateAvgSegmentSize(tasks)
	avgMemoryFactor := b.getAvgMemoryFactor(tasks)
	memoryPerBuild := avgSegmentSize * avgMemoryFactor

	maxByMemory := int(availableMemory / memoryPerBuild)

	// Take minimum (most restrictive constraint)
	optimal := min(maxByCPU, maxByMemory)

	// Ensure at least 1, at most maxConcurrent
	optimal = max(1, min(optimal, int(b.maxConcurrent)))

	log.Info("Concurrency calculation",
		zap.Int("maxByCPU", maxByCPU),
		zap.Int("maxByMemory", maxByMemory),
		zap.Int("optimal", optimal),
		zap.Float64("avgMemoryPerBuild_GB", memoryPerBuild/1024/1024/1024))

	return optimal
}

// calculateMemoryWeight returns semaphore weight (1-100) for a task
func (b *ParallelIndexBuilder) calculateMemoryWeight(task *indexBuildTask) int64 {
	segmentSize := float64(b.getSegmentSize(task)) // bytes
	indexType := task.newIndexParams[common.IndexTypeKey]
	memoryFactor := b.getMemoryFactor(indexType)

	estimatedMemory := segmentSize * memoryFactor

	// Total available memory
	totalMemory := float64(hardware.GetMemoryCount())
	reservedMemory := totalMemory * b.config.MemoryReservationRatio

	// Weight = (task memory / reserved memory) * 100
	// This ensures total weight of concurrent tasks <= 100
	weight := int64((estimatedMemory / reservedMemory) * 100)

	// Clamp to reasonable range [1, 100]
	weight = max(1, min(100, weight))

	return weight
}

// getMemoryFactor returns memory multiplier for index type
func (b *ParallelIndexBuilder) getMemoryFactor(indexType string) float64 {
	if factor, ok := b.config.MemoryFactors[indexType]; ok {
		return factor
	}

	// Default factors based on empirical measurements
	switch indexType {
	case "HNSW":
		return 1.5 // HNSW uses ~1.5x raw data size during build
	case "IVF_FLAT":
		return 2.0 // IVF needs more memory for clustering
	case "IVF_PQ":
		return 1.8 // Slightly less than IVF_FLAT
	case "IVF_SQ8":
		return 1.7
	case "DiskANN":
		return 1.2 // More memory-efficient
	case "FLAT":
		return 1.1
	default:
		return 1.5 // Conservative default
	}
}

// getSegmentSize estimates segment size from task data
func (b *ParallelIndexBuilder) getSegmentSize(task *indexBuildTask) uint64 {
	// Estimate size based on dimensions and number of rows
	dim := task.req.GetDim()
	numRows := task.req.GetNumRows()

	// Rough estimate: each vector is dim * 4 bytes (assuming float32)
	estimatedSize := uint64(dim) * uint64(numRows) * 4

	return estimatedSize
}

// calculateAvgSegmentSize returns average segment size in bytes
func (b *ParallelIndexBuilder) calculateAvgSegmentSize(tasks []*indexBuildTask) float64 {
	if len(tasks) == 0 {
		return 0
	}

	total := uint64(0)
	for _, task := range tasks {
		total += b.getSegmentSize(task)
	}

	return float64(total) / float64(len(tasks))
}

// getAvgMemoryFactor returns average memory factor across tasks
func (b *ParallelIndexBuilder) getAvgMemoryFactor(tasks []*indexBuildTask) float64 {
	if len(tasks) == 0 {
		return 1.5
	}

	total := 0.0
	for _, task := range tasks {
		indexType := task.newIndexParams[common.IndexTypeKey]
		total += b.getMemoryFactor(indexType)
	}

	return total / float64(len(tasks))
}

// buildSequential is fallback for single task or disabled parallel
func (b *ParallelIndexBuilder) buildSequential(
	ctx context.Context,
	tasks []*indexBuildTask,
) error {
	for i, task := range tasks {
		segmentID := task.req.GetSegmentID()

		log.Info("Building index sequentially",
			zap.Int("taskIndex", i),
			zap.Int64("segmentID", segmentID))

		if err := task.Execute(ctx); err != nil {
			return fmt.Errorf("task %d failed: %w", i, err)
		}
	}

	return nil
}

// Close releases resources held by the builder
func (b *ParallelIndexBuilder) Close() {
	if b.workerPool != nil {
		b.workerPool.Release()
	}
}

// defaultParallelConfig returns default configuration
func defaultParallelConfig() *ParallelConfig {
	return &ParallelConfig{
		MaxConcurrentBuilds:    0,   // Auto-detect
		MemoryReservationRatio: 0.8, // Use 80% of memory
		MemoryFactors: map[string]float64{
			"HNSW":     1.5,
			"IVF_FLAT": 2.0,
			"IVF_PQ":   1.8,
			"IVF_SQ8":  1.7,
			"DiskANN":  1.2,
			"FLAT":     1.1,
		},
		Enabled: true,
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
