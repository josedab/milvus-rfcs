// Copyright (C) 2019-2020 Zilliz. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hardware

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/metrics"
	"github.com/milvus-io/milvus/pkg/v2/util/paramtable"
)

const (
	// Default monitoring interval
	defaultMonitorInterval = 10 * time.Second

	// Memory leak detection threshold (bytes per hour)
	// Alert if growing more than 100MB/hour continuously
	memoryLeakThreshold = 100 * 1024 * 1024

	// Memory usage warning thresholds
	memoryWarningThreshold  = 0.80 // 80%
	memoryCriticalThreshold = 0.90 // 90%
)

// MemoryMonitor monitors memory usage and provides metrics
type MemoryMonitor struct {
	// Baseline for leak detection
	baselineMemory  uint64
	lastMeasurement time.Time

	// Synchronization
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// Configuration
	monitorInterval time.Duration
}

// NewMemoryMonitor creates a new memory monitor instance
func NewMemoryMonitor() *MemoryMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryMonitor{
		ctx:             ctx,
		cancel:          cancel,
		monitorInterval: defaultMonitorInterval,
		lastMeasurement: time.Now(),
	}
}

// Start begins the memory monitoring loop
func (m *MemoryMonitor) Start() {
	log.Info("Starting memory monitor",
		zap.Duration("interval", m.monitorInterval))

	go m.monitorLoop()
}

// Stop halts the memory monitoring loop
func (m *MemoryMonitor) Stop() {
	log.Info("Stopping memory monitor")
	m.cancel()
}

// monitorLoop runs periodic memory checks
func (m *MemoryMonitor) monitorLoop() {
	ticker := time.NewTicker(m.monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			log.Info("Memory monitor stopped")
			return
		case <-ticker.C:
			m.collectMetrics()
			m.detectLeaks()
			m.checkThresholds()
		}
	}
}

// collectMetrics gathers and reports memory metrics
func (m *MemoryMonitor) collectMetrics() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	nodeID := getNodeIDString()

	// Total allocated memory
	metrics.ComponentMemory.WithLabelValues("total", nodeID).
		Set(float64(stats.Alloc))

	// Heap memory
	metrics.ComponentMemory.WithLabelValues("heap", nodeID).
		Set(float64(stats.HeapAlloc))

	// Stack memory
	metrics.ComponentMemory.WithLabelValues("stack", nodeID).
		Set(float64(stats.StackInuse))

	// GC-related memory
	metrics.ComponentMemory.WithLabelValues("gc_sys", nodeID).
		Set(float64(stats.GCSys))

	// Calculate and report memory usage percentage
	totalMemory := GetMemoryCount()
	if totalMemory > 0 {
		usagePercent := float64(stats.Alloc) / float64(totalMemory) * 100
		metrics.MemoryUsagePercent.WithLabelValues(nodeID).Set(usagePercent)
	}
}

// detectLeaks monitors memory growth rate and detects potential leaks
func (m *MemoryMonitor) detectLeaks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	current := stats.Alloc

	if m.baselineMemory > 0 {
		// Check growth rate
		timeSince := time.Since(m.lastMeasurement)
		if timeSince.Hours() > 0 {
			growth := int64(current) - int64(m.baselineMemory)
			growthRate := float64(growth) / timeSince.Hours() // bytes per hour

			nodeID := getNodeIDString()
			metrics.MemoryGrowthRate.WithLabelValues(nodeID).Set(growthRate)

			// Alert if growing more than threshold continuously
			if growthRate > memoryLeakThreshold {
				log.Warn("Potential memory leak detected",
					zap.Float64("growth_rate_mb_per_hour", growthRate/(1024*1024)),
					zap.Uint64("baseline_bytes", m.baselineMemory),
					zap.Uint64("current_bytes", current),
					zap.Float64("hours_elapsed", timeSince.Hours()))
			}
		}
	}

	m.baselineMemory = current
	m.lastMeasurement = time.Now()
}

// checkThresholds monitors memory usage against configured thresholds
func (m *MemoryMonitor) checkThresholds() {
	totalMemory := GetMemoryCount()
	if totalMemory == 0 {
		return
	}

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	usage := float64(stats.Alloc) / float64(totalMemory)

	nodeID := getNodeIDString()

	if usage > memoryCriticalThreshold {
		log.Error("Critical memory usage",
			zap.Float64("usage_percent", usage*100),
			zap.Uint64("used_bytes", stats.Alloc),
			zap.Uint64("total_bytes", totalMemory),
			zap.String("node_id", nodeID))
	} else if usage > memoryWarningThreshold {
		log.Warn("High memory usage",
			zap.Float64("usage_percent", usage*100),
			zap.Uint64("used_bytes", stats.Alloc),
			zap.Uint64("total_bytes", totalMemory),
			zap.String("node_id", nodeID))
	}
}

// getNodeIDString returns the node ID as a string
func getNodeIDString() string {
	return fmt.Sprint(paramtable.GetNodeID())
}

// RecordIndexMemory records memory usage for a specific index
func RecordIndexMemory(indexType string, collectionID int64, memoryBytes uint64) {
	metrics.IndexMemory.WithLabelValues(
		indexType,
		fmt.Sprint(collectionID),
	).Set(float64(memoryBytes))
}

// RecordSegmentMemory records memory usage for a specific segment
func RecordSegmentMemory(segmentID int64, collectionID int64, memoryBytes uint64) {
	metrics.SegmentMemory.WithLabelValues(
		fmt.Sprint(segmentID),
		fmt.Sprint(collectionID),
	).Set(float64(memoryBytes))
}
