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

package optimization

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetricsCollector_RecordQuery(t *testing.T) {
	mc := NewMetricsCollector(24*time.Hour, 1000)

	// Record a query
	metrics := QueryMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		Latency:      50 * time.Millisecond,
		Recall:       0.95,
		MemoryUsage:  1024 * 1024,
		CPUUsage:     0.5,
		SearchParams: map[string]interface{}{"ef": 64},
		IndexParams:  map[string]interface{}{"M": 16},
	}

	mc.RecordQuery(metrics)

	// Verify metrics were recorded
	collectionIDs := mc.GetAllCollectionIDs()
	assert.Len(t, collectionIDs, 1)
	assert.Equal(t, int64(1), collectionIDs[0])
}

func TestMetricsCollector_GetCollectionMetrics(t *testing.T) {
	mc := NewMetricsCollector(24*time.Hour, 1000)

	// Record multiple queries
	for i := 0; i < 100; i++ {
		latency := time.Duration(40+i/10) * time.Millisecond
		recall := 0.90 + float64(i)/1000

		metrics := QueryMetrics{
			CollectionID: 1,
			IndexType:    "HNSW",
			Latency:      latency,
			Recall:       recall,
			MemoryUsage:  1024 * 1024,
			CPUUsage:     0.5,
			SearchParams: map[string]interface{}{"ef": 64},
			IndexParams:  map[string]interface{}{"M": 16},
		}
		mc.RecordQuery(metrics)
	}

	// Get aggregated metrics
	aggMetrics := mc.GetCollectionMetrics(1, 24*time.Hour)
	assert.NotNil(t, aggMetrics)
	assert.Equal(t, int64(1), aggMetrics.CollectionID)
	assert.Equal(t, "HNSW", aggMetrics.IndexType)
	assert.Equal(t, int64(100), aggMetrics.SampleCount)

	// Verify latency percentiles are reasonable
	assert.Greater(t, aggMetrics.P95Latency, aggMetrics.P50Latency)
	assert.Greater(t, aggMetrics.P99Latency, aggMetrics.P95Latency)

	// Verify recall metrics
	assert.Greater(t, aggMetrics.MeanRecall, 0.90)
	assert.Less(t, aggMetrics.MeanRecall, 1.0)
}

func TestMetricsCollector_RetentionPolicy(t *testing.T) {
	mc := NewMetricsCollector(1*time.Hour, 1000)

	// Record old metrics
	oldMetrics := QueryMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		Latency:      50 * time.Millisecond,
		Recall:       0.95,
		Timestamp:    time.Now().Add(-2 * time.Hour), // 2 hours ago
	}
	mc.mu.Lock()
	mc.metrics[1] = []QueryMetrics{oldMetrics}
	mc.mu.Unlock()

	// Record new metrics
	newMetrics := QueryMetrics{
		CollectionID: 1,
		IndexType:    "HNSW",
		Latency:      50 * time.Millisecond,
		Recall:       0.95,
	}
	mc.RecordQuery(newMetrics)

	// Old metrics should be cleaned up
	aggMetrics := mc.GetCollectionMetrics(1, 1*time.Hour)
	assert.NotNil(t, aggMetrics)
	assert.Equal(t, int64(1), aggMetrics.SampleCount) // Only new metrics
}

func TestMetricsCollector_MaxSamples(t *testing.T) {
	mc := NewMetricsCollector(24*time.Hour, 10) // Max 10 samples

	// Record 20 queries
	for i := 0; i < 20; i++ {
		metrics := QueryMetrics{
			CollectionID: 1,
			IndexType:    "HNSW",
			Latency:      50 * time.Millisecond,
			Recall:       0.95,
		}
		mc.RecordQuery(metrics)
	}

	// Should only keep 10 most recent
	aggMetrics := mc.GetCollectionMetrics(1, 24*time.Hour)
	assert.NotNil(t, aggMetrics)
	assert.Equal(t, int64(10), aggMetrics.SampleCount)
}

func TestMetricsCollector_CleanupOldMetrics(t *testing.T) {
	mc := NewMetricsCollector(1*time.Hour, 1000)

	// Add old metrics
	mc.mu.Lock()
	mc.metrics[1] = []QueryMetrics{
		{
			CollectionID: 1,
			Timestamp:    time.Now().Add(-2 * time.Hour),
		},
	}
	mc.metrics[2] = []QueryMetrics{
		{
			CollectionID: 2,
			Timestamp:    time.Now().Add(-30 * time.Minute),
		},
	}
	mc.mu.Unlock()

	// Cleanup
	mc.CleanupOldMetrics()

	// Collection 1 should be removed, Collection 2 should remain
	collectionIDs := mc.GetAllCollectionIDs()
	assert.Len(t, collectionIDs, 1)
	assert.Equal(t, int64(2), collectionIDs[0])
}

func TestSortDurations(t *testing.T) {
	durations := []time.Duration{
		100 * time.Millisecond,
		50 * time.Millisecond,
		200 * time.Millisecond,
		75 * time.Millisecond,
	}

	sortDurations(durations)

	assert.Equal(t, 50*time.Millisecond, durations[0])
	assert.Equal(t, 75*time.Millisecond, durations[1])
	assert.Equal(t, 100*time.Millisecond, durations[2])
	assert.Equal(t, 200*time.Millisecond, durations[3])
}
