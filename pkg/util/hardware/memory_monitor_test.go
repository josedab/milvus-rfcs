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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/milvus-io/milvus/pkg/v2/metrics"
)

func TestNewMemoryMonitor(t *testing.T) {
	monitor := NewMemoryMonitor()
	assert.NotNil(t, monitor)
	assert.NotNil(t, monitor.ctx)
	assert.NotNil(t, monitor.cancel)
	assert.Equal(t, defaultMonitorInterval, monitor.monitorInterval)
}

func TestMemoryMonitorStartStop(t *testing.T) {
	monitor := NewMemoryMonitor()
	require.NotNil(t, monitor)

	// Start the monitor
	monitor.Start()

	// Let it run for a short time
	time.Sleep(100 * time.Millisecond)

	// Stop the monitor
	monitor.Stop()

	// Ensure context is cancelled
	select {
	case <-monitor.ctx.Done():
		// Context should be done
	case <-time.After(1 * time.Second):
		t.Fatal("Monitor did not stop within timeout")
	}
}

func TestCollectMetrics(t *testing.T) {
	monitor := NewMemoryMonitor()
	require.NotNil(t, monitor)

	// Collect metrics once
	monitor.collectMetrics()

	// Verify that metrics were set (we can't check exact values, but we can verify no panic)
	// The metrics should be > 0 since the test itself uses memory
}

func TestDetectLeaks(t *testing.T) {
	monitor := NewMemoryMonitor()
	require.NotNil(t, monitor)

	// First call should set baseline
	monitor.detectLeaks()
	assert.Greater(t, monitor.baselineMemory, uint64(0))
	firstBaseline := monitor.baselineMemory

	// Second call should update baseline
	time.Sleep(10 * time.Millisecond)
	monitor.detectLeaks()
	assert.GreaterOrEqual(t, monitor.baselineMemory, firstBaseline)
}

func TestCheckThresholds(t *testing.T) {
	monitor := NewMemoryMonitor()
	require.NotNil(t, monitor)

	// This should not panic
	monitor.checkThresholds()
}

func TestRecordIndexMemory(t *testing.T) {
	tests := []struct {
		name         string
		indexType    string
		collectionID int64
		memoryBytes  uint64
	}{
		{
			name:         "HNSW index",
			indexType:    "HNSW",
			collectionID: 1,
			memoryBytes:  1024 * 1024 * 100, // 100 MB
		},
		{
			name:         "IVF_FLAT index",
			indexType:    "IVF_FLAT",
			collectionID: 2,
			memoryBytes:  1024 * 1024 * 50, // 50 MB
		},
		{
			name:         "Zero memory",
			indexType:    "HNSW",
			collectionID: 3,
			memoryBytes:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			RecordIndexMemory(tt.indexType, tt.collectionID, tt.memoryBytes)
		})
	}
}

func TestRecordSegmentMemory(t *testing.T) {
	tests := []struct {
		name         string
		segmentID    int64
		collectionID int64
		memoryBytes  uint64
	}{
		{
			name:         "Normal segment",
			segmentID:    1001,
			collectionID: 1,
			memoryBytes:  1024 * 1024 * 200, // 200 MB
		},
		{
			name:         "Large segment",
			segmentID:    1002,
			collectionID: 2,
			memoryBytes:  1024 * 1024 * 1024, // 1 GB
		},
		{
			name:         "Released segment",
			segmentID:    1003,
			collectionID: 3,
			memoryBytes:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			RecordSegmentMemory(tt.segmentID, tt.collectionID, tt.memoryBytes)
		})
	}
}

func TestMemoryMonitorConcurrency(t *testing.T) {
	monitor := NewMemoryMonitor()
	require.NotNil(t, monitor)

	monitor.Start()
	defer monitor.Stop()

	// Simulate concurrent operations
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			RecordIndexMemory("HNSW", int64(i), uint64(i*1024))
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			RecordSegmentMemory(int64(i), 1, uint64(i*2048))
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Monitor should still be running
	select {
	case <-monitor.ctx.Done():
		t.Fatal("Monitor stopped unexpectedly")
	default:
		// Still running, as expected
	}
}

func TestMemoryMonitorLoop(t *testing.T) {
	// Create a monitor with shorter interval for testing
	monitor := NewMemoryMonitor()
	monitor.monitorInterval = 50 * time.Millisecond

	monitor.Start()

	// Let it run through a few cycles
	time.Sleep(200 * time.Millisecond)

	// Verify that monitoring has been performed
	assert.Greater(t, monitor.baselineMemory, uint64(0))
	assert.False(t, monitor.lastMeasurement.IsZero())

	// Stop the monitor
	monitor.Stop()

	// Verify it stopped
	select {
	case <-monitor.ctx.Done():
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Monitor did not stop")
	}
}

func TestGetNodeIDString(t *testing.T) {
	nodeID := getNodeIDString()
	assert.NotEmpty(t, nodeID)
	// Should be a valid string representation of a number
}

func TestMemoryMetricsRegistration(t *testing.T) {
	// Verify that all memory metrics are defined
	assert.NotNil(t, metrics.ComponentMemory)
	assert.NotNil(t, metrics.IndexMemory)
	assert.NotNil(t, metrics.SegmentMemory)
	assert.NotNil(t, metrics.MemoryUsagePercent)
	assert.NotNil(t, metrics.MemoryGrowthRate)
}

func BenchmarkCollectMetrics(b *testing.B) {
	monitor := NewMemoryMonitor()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		monitor.collectMetrics()
	}
}

func BenchmarkDetectLeaks(b *testing.B) {
	monitor := NewMemoryMonitor()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		monitor.detectLeaks()
	}
}

func BenchmarkRecordIndexMemory(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		RecordIndexMemory("HNSW", int64(i%100), uint64(i*1024))
	}
}

func BenchmarkRecordSegmentMemory(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		RecordSegmentMemory(int64(i%1000), int64(i%10), uint64(i*2048))
	}
}
