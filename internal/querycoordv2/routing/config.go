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

package routing

import (
	"time"

	"github.com/milvus-io/milvus/pkg/v2/util/paramtable"
)

// LoadConfigFromParams creates a RouterConfig from the global parameter table
func LoadConfigFromParams() *RouterConfig {
	params := paramtable.Get()

	return &RouterConfig{
		CPUWeight:             params.QueryCoordCfg.AdaptiveRoutingCPUWeight.GetAsFloat(),
		MemoryWeight:          params.QueryCoordCfg.AdaptiveRoutingMemoryWeight.GetAsFloat(),
		CacheWeight:           params.QueryCoordCfg.AdaptiveRoutingCacheWeight.GetAsFloat(),
		LatencyWeight:         params.QueryCoordCfg.AdaptiveRoutingLatencyWeight.GetAsFloat(),
		MaxCPUUsage:           params.QueryCoordCfg.AdaptiveRoutingMaxCPUUsage.GetAsFloat(),
		MaxMemoryUsage:        params.QueryCoordCfg.AdaptiveRoutingMaxMemoryUsage.GetAsFloat(),
		MinHealthScore:        params.QueryCoordCfg.AdaptiveRoutingMinHealthScore.GetAsFloat(),
		MetricsUpdateInterval: time.Duration(params.QueryCoordCfg.AdaptiveRoutingMetricsInterval.GetAsInt64()) * time.Second,
		RebalanceInterval:     time.Duration(params.QueryCoordCfg.AdaptiveRoutingRebalanceInterval.GetAsInt64()) * time.Second,
	}
}

// IsAdaptiveRoutingEnabled checks if adaptive routing is enabled
func IsAdaptiveRoutingEnabled() bool {
	params := paramtable.Get()
	return params.QueryCoordCfg.EnableAdaptiveRouting.GetAsBool()
}
