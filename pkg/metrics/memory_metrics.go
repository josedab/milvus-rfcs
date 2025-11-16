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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// ComponentMemory tracks memory usage by component (total, heap, stack)
	ComponentMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: milvusNamespace,
			Name:      "component_memory_bytes",
			Help:      "Memory usage by component in bytes",
		},
		[]string{"component", nodeIDLabelName},
	)

	// IndexMemory tracks memory usage by index type
	IndexMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: milvusNamespace,
			Name:      "index_memory_bytes",
			Help:      "Memory usage by index type in bytes",
		},
		[]string{"index_type", collectionIDLabelName},
	)

	// SegmentMemory tracks memory usage by segment
	SegmentMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: milvusNamespace,
			Name:      "segment_memory_bytes",
			Help:      "Memory usage by segment in bytes",
		},
		[]string{"segment_id", collectionIDLabelName},
	)

	// MemoryUsagePercent tracks the overall memory usage as a percentage
	MemoryUsagePercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: milvusNamespace,
			Name:      "memory_usage_percent",
			Help:      "Memory usage as a percentage of total available memory",
		},
		[]string{nodeIDLabelName},
	)

	// MemoryGrowthRate tracks the rate of memory growth
	MemoryGrowthRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: milvusNamespace,
			Name:      "memory_growth_bytes_per_hour",
			Help:      "Memory growth rate in bytes per hour",
		},
		[]string{nodeIDLabelName},
	)
)

// RegisterMemoryMetrics registers all memory monitoring metrics
func RegisterMemoryMetrics(registry *prometheus.Registry) {
	registry.MustRegister(ComponentMemory)
	registry.MustRegister(IndexMemory)
	registry.MustRegister(SegmentMemory)
	registry.MustRegister(MemoryUsagePercent)
	registry.MustRegister(MemoryGrowthRate)
}
