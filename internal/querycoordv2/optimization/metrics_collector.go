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
	"sync"
	"time"
)

// QueryMetrics represents performance metrics for a single query
type QueryMetrics struct {
	CollectionID   int64
	IndexType      string
	Latency        time.Duration
	Recall         float64
	MemoryUsage    int64 // in bytes
	CPUUsage       float64
	SearchParams   map[string]interface{}
	IndexParams    map[string]interface{}
	Timestamp      time.Time
}

// CollectionMetrics represents aggregated metrics for a collection
type CollectionMetrics struct {
	CollectionID       int64
	CollectionName     string
	IndexType          string

	// Latency metrics
	P50Latency         time.Duration
	P95Latency         time.Duration
	P99Latency         time.Duration
	MeanLatency        time.Duration

	// Recall metrics
	MeanRecall         float64
	MinRecall          float64

	// Resource metrics
	MemoryUsage        int64
	CPUUsage           float64

	// Parameters
	SearchParams       map[string]interface{}
	IndexParams        map[string]interface{}

	// Sample count
	SampleCount        int64
	TimeWindow         time.Duration
	LastUpdated        time.Time
}

// MetricsCollector collects and aggregates query performance metrics
type MetricsCollector struct {
	mu              sync.RWMutex
	metrics         map[int64][]QueryMetrics  // collectionID -> metrics
	retentionPeriod time.Duration
	maxSamples      int
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(retentionPeriod time.Duration, maxSamples int) *MetricsCollector {
	return &MetricsCollector{
		metrics:         make(map[int64][]QueryMetrics),
		retentionPeriod: retentionPeriod,
		maxSamples:      maxSamples,
	}
}

// RecordQuery records metrics for a single query
func (mc *MetricsCollector) RecordQuery(metrics QueryMetrics) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics.Timestamp = time.Now()
	collectionMetrics := mc.metrics[metrics.CollectionID]

	// Add new metrics
	collectionMetrics = append(collectionMetrics, metrics)

	// Enforce retention policy (time-based)
	cutoff := time.Now().Add(-mc.retentionPeriod)
	validMetrics := make([]QueryMetrics, 0, len(collectionMetrics))
	for _, m := range collectionMetrics {
		if m.Timestamp.After(cutoff) {
			validMetrics = append(validMetrics, m)
		}
	}

	// Enforce sample limit
	if len(validMetrics) > mc.maxSamples {
		validMetrics = validMetrics[len(validMetrics)-mc.maxSamples:]
	}

	mc.metrics[metrics.CollectionID] = validMetrics
}

// GetCollectionMetrics returns aggregated metrics for a collection
func (mc *MetricsCollector) GetCollectionMetrics(collectionID int64, timeWindow time.Duration) *CollectionMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	rawMetrics := mc.metrics[collectionID]
	if len(rawMetrics) == 0 {
		return nil
	}

	// Filter by time window
	cutoff := time.Now().Add(-timeWindow)
	var filteredMetrics []QueryMetrics
	for _, m := range rawMetrics {
		if m.Timestamp.After(cutoff) {
			filteredMetrics = append(filteredMetrics, m)
		}
	}

	if len(filteredMetrics) == 0 {
		return nil
	}

	return mc.aggregateMetrics(collectionID, filteredMetrics, timeWindow)
}

// aggregateMetrics computes aggregated statistics from raw metrics
func (mc *MetricsCollector) aggregateMetrics(collectionID int64, metrics []QueryMetrics, timeWindow time.Duration) *CollectionMetrics {
	if len(metrics) == 0 {
		return nil
	}

	// Sort latencies for percentile calculation
	latencies := make([]time.Duration, len(metrics))
	recalls := make([]float64, len(metrics))
	var totalLatency time.Duration
	var totalRecall float64
	var totalMemory int64
	var totalCPU float64
	minRecall := 1.0

	for i, m := range metrics {
		latencies[i] = m.Latency
		recalls[i] = m.Recall
		totalLatency += m.Latency
		totalRecall += m.Recall
		totalMemory += m.MemoryUsage
		totalCPU += m.CPUUsage

		if m.Recall < minRecall {
			minRecall = m.Recall
		}
	}

	// Sort for percentiles
	sortDurations(latencies)

	n := len(metrics)
	result := &CollectionMetrics{
		CollectionID:  collectionID,
		IndexType:     metrics[0].IndexType,
		P50Latency:    latencies[n*50/100],
		P95Latency:    latencies[n*95/100],
		P99Latency:    latencies[n*99/100],
		MeanLatency:   totalLatency / time.Duration(n),
		MeanRecall:    totalRecall / float64(n),
		MinRecall:     minRecall,
		MemoryUsage:   totalMemory / int64(n),
		CPUUsage:      totalCPU / float64(n),
		SearchParams:  metrics[len(metrics)-1].SearchParams,
		IndexParams:   metrics[len(metrics)-1].IndexParams,
		SampleCount:   int64(n),
		TimeWindow:    timeWindow,
		LastUpdated:   time.Now(),
	}

	return result
}

// sortDurations sorts a slice of durations in place
func sortDurations(durations []time.Duration) {
	// Simple insertion sort (sufficient for small datasets)
	for i := 1; i < len(durations); i++ {
		key := durations[i]
		j := i - 1
		for j >= 0 && durations[j] > key {
			durations[j+1] = durations[j]
			j--
		}
		durations[j+1] = key
	}
}

// CleanupOldMetrics removes metrics older than retention period
func (mc *MetricsCollector) CleanupOldMetrics() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	cutoff := time.Now().Add(-mc.retentionPeriod)

	for collectionID, metrics := range mc.metrics {
		validMetrics := make([]QueryMetrics, 0, len(metrics))
		for _, m := range metrics {
			if m.Timestamp.After(cutoff) {
				validMetrics = append(validMetrics, m)
			}
		}

		if len(validMetrics) == 0 {
			delete(mc.metrics, collectionID)
		} else {
			mc.metrics[collectionID] = validMetrics
		}
	}
}

// GetAllCollectionIDs returns all collection IDs with metrics
func (mc *MetricsCollector) GetAllCollectionIDs() []int64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	collectionIDs := make([]int64, 0, len(mc.metrics))
	for id := range mc.metrics {
		collectionIDs = append(collectionIDs, id)
	}
	return collectionIDs
}
