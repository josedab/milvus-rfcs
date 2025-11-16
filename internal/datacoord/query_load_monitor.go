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

// QueryLoadMonitor monitors query load (QPS) to help make compaction decisions
type QueryLoadMonitor struct {
	mu sync.RWMutex

	// Rolling window of QPS measurements
	qpsWindow    []int64
	windowSize   int
	currentIndex int

	// Update interval
	updateTicker *time.Ticker
	stopChan     chan struct{}
}

// NewQueryLoadMonitor creates a new query load monitor
func NewQueryLoadMonitor() *QueryLoadMonitor {
	monitor := &QueryLoadMonitor{
		qpsWindow:  make([]int64, 60), // 60-second window
		windowSize: 60,
		stopChan:   make(chan struct{}),
	}

	// Start background updater
	monitor.updateTicker = time.NewTicker(1 * time.Second)
	go monitor.updateLoop()

	return monitor
}

// CurrentQPS returns average QPS over the last minute
func (m *QueryLoadMonitor) CurrentQPS() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sum := int64(0)
	count := 0

	for _, qps := range m.qpsWindow {
		if qps > 0 {
			sum += qps
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return sum / int64(count)
}

// updateLoop periodically fetches QPS from metrics
func (m *QueryLoadMonitor) updateLoop() {
	for {
		select {
		case <-m.updateTicker.C:
			qps := m.fetchCurrentQPS()

			m.mu.Lock()
			m.qpsWindow[m.currentIndex] = qps
			m.currentIndex = (m.currentIndex + 1) % m.windowSize
			m.mu.Unlock()
		case <-m.stopChan:
			return
		}
	}
}

// fetchCurrentQPS queries metrics for current QPS
// This is a placeholder implementation - actual implementation would
// query Prometheus metrics or internal QPS counters
func (m *QueryLoadMonitor) fetchCurrentQPS() int64 {
	// TODO: Implement actual QPS fetching from metrics
	// For now, return 0 to indicate no load
	// In production, this should query:
	// - Prometheus metrics for QueryNode search/query QPS
	// - QueryCoord statistics
	// - Or internal rate counters

	params := paramtable.Get()
	if params.DataCoordCfg.SmartCompactionEnabled.GetAsBool() {
		log.Debug("Fetching current QPS for smart compaction")
	}

	// Placeholder: return 0
	// Real implementation would use something like:
	// return metrics.QueryNodeSearchQPS.GetTotal()
	return 0
}

// RecordQPS manually records a QPS value (for testing or manual tracking)
func (m *QueryLoadMonitor) RecordQPS(qps int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.qpsWindow[m.currentIndex] = qps
	m.currentIndex = (m.currentIndex + 1) % m.windowSize

	log.Debug("Recorded QPS",
		zap.Int64("qps", qps),
		zap.Int64("avgQPS", m.currentQPSLocked()))
}

// currentQPSLocked returns current QPS without locking (caller must hold lock)
func (m *QueryLoadMonitor) currentQPSLocked() int64 {
	sum := int64(0)
	count := 0

	for _, qps := range m.qpsWindow {
		if qps > 0 {
			sum += qps
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return sum / int64(count)
}

// Stop stops the background update loop
func (m *QueryLoadMonitor) Stop() {
	if m.updateTicker != nil {
		m.updateTicker.Stop()
	}
	close(m.stopChan)
}
