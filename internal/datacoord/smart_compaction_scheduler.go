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
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/util/paramtable"
)

// SmartCompactionScheduler determines when compaction should run based on
// query load, time of day, and segment access patterns
type SmartCompactionScheduler struct {
	queryLoadMonitor *QueryLoadMonitor
	segmentAnalyzer  *ColdSegmentAnalyzer

	// State tracking
	lastCompaction time.Time
	deferredCount  int
}

// CompactionDecision represents the decision about whether to run compaction
type CompactionDecision struct {
	ShouldRun      bool
	Reason         string
	Priority       string    // "high", "normal", "low"
	DeferUntil     time.Time
	TargetSegments []int64 // Specific segments to compact
}

// NewSmartCompactionScheduler creates a new smart compaction scheduler
func NewSmartCompactionScheduler() *SmartCompactionScheduler {
	return &SmartCompactionScheduler{
		queryLoadMonitor: NewQueryLoadMonitor(),
		segmentAnalyzer:  NewColdSegmentAnalyzer(),
		lastCompaction:   time.Now(),
		deferredCount:    0,
	}
}

// ShouldCompact determines if compaction should run now
func (s *SmartCompactionScheduler) ShouldCompact(
	ctx context.Context,
	compactionType string,
) CompactionDecision {
	params := paramtable.Get()

	// Check if smart scheduling is enabled
	if !params.DataCoordCfg.SmartCompactionEnabled.GetAsBool() {
		// Fallback to time-based
		return CompactionDecision{
			ShouldRun: true,
			Reason:    "smart scheduling disabled",
		}
	}

	// Check 1: Has compaction been deferred too long?
	if s.mustCompactNow() {
		return CompactionDecision{
			ShouldRun: true,
			Reason:    "max deferral reached",
			Priority:  "high",
		}
	}

	// Check 2: Query load
	currentQPS := s.queryLoadMonitor.CurrentQPS()
	peakQPSThreshold := params.DataCoordCfg.SmartCompactionPeakQPSThreshold.GetAsInt64()
	if currentQPS > peakQPSThreshold {
		s.deferCompaction("high query load")
		return CompactionDecision{
			ShouldRun: false,
			Reason: fmt.Sprintf("QPS %d > threshold %d",
				currentQPS, peakQPSThreshold),
			DeferUntil: time.Now().Add(5 * time.Minute),
		}
	}

	// Check 3: Time of day
	currentHour := time.Now().Hour()
	if s.isPeakHour(currentHour) {
		s.deferCompaction("peak hours")
		return CompactionDecision{
			ShouldRun:  false,
			Reason:     fmt.Sprintf("peak hour %d", currentHour),
			DeferUntil: s.nextOffPeakTime(),
		}
	}

	// Check 4: Cold segment analysis
	minColdSegments := params.DataCoordCfg.SmartCompactionMinColdSegments.GetAsInt()
	if params.DataCoordCfg.SmartCompactionColdSegmentEnabled.GetAsBool() {
		coldSegments := s.segmentAnalyzer.IdentifyColdSegments()
		if len(coldSegments) < minColdSegments {
			// Not enough cold segments, can defer
			return CompactionDecision{
				ShouldRun: false,
				Reason: fmt.Sprintf("only %d cold segments (need %d)",
					len(coldSegments), minColdSegments),
				DeferUntil: time.Now().Add(10 * time.Minute),
			}
		}

		// All conditions met - run compaction on cold segments
		log.Info("Smart compaction triggered",
			zap.String("type", compactionType),
			zap.Int64("qps", currentQPS),
			zap.Int("hour", currentHour),
			zap.Int("coldSegments", len(coldSegments)))

		s.lastCompaction = time.Now()
		s.deferredCount = 0

		return CompactionDecision{
			ShouldRun:      true,
			Reason:         "optimal conditions with cold segments",
			Priority:       "normal",
			TargetSegments: coldSegments,
		}
	}

	// All conditions met - run compaction
	log.Info("Smart compaction triggered",
		zap.String("type", compactionType),
		zap.Int64("qps", currentQPS),
		zap.Int("hour", currentHour))

	s.lastCompaction = time.Now()
	s.deferredCount = 0

	return CompactionDecision{
		ShouldRun: true,
		Reason:    "optimal conditions",
		Priority:  "normal",
	}
}

// mustCompactNow checks if we've deferred too long
func (s *SmartCompactionScheduler) mustCompactNow() bool {
	params := paramtable.Get()

	// Check 1: Time-based limit
	maxDeferralTime := params.DataCoordCfg.SmartCompactionMaxDeferralHours.GetAsDuration(time.Hour)
	if time.Since(s.lastCompaction) > maxDeferralTime {
		log.Warn("Force compaction: max deferral time exceeded",
			zap.Duration("since", time.Since(s.lastCompaction)),
			zap.Duration("limit", maxDeferralTime))
		return true
	}

	// Check 2: Count-based limit
	maxDeferralCount := params.DataCoordCfg.SmartCompactionMaxDeferralCount.GetAsInt()
	if s.deferredCount >= maxDeferralCount {
		log.Warn("Force compaction: max deferral count exceeded",
			zap.Int("count", s.deferredCount),
			zap.Int("limit", maxDeferralCount))
		return true
	}

	return false
}

// deferCompaction increments deferral tracking
func (s *SmartCompactionScheduler) deferCompaction(reason string) {
	s.deferredCount++
	log.Debug("Compaction deferred",
		zap.String("reason", reason),
		zap.Int("deferralCount", s.deferredCount))
}

// isPeakHour checks if current hour is in peak hours
func (s *SmartCompactionScheduler) isPeakHour(hour int) bool {
	peakHours := paramtable.Get().DataCoordCfg.SmartCompactionPeakHours.GetAsStrings()
	for _, peakHourStr := range peakHours {
		var peakHour int
		if _, err := fmt.Sscanf(peakHourStr, "%d", &peakHour); err == nil {
			if hour == peakHour {
				return true
			}
		}
	}
	return false
}

// nextOffPeakTime calculates next off-peak window
func (s *SmartCompactionScheduler) nextOffPeakTime() time.Time {
	now := time.Now()

	for i := 1; i <= 24; i++ {
		nextHour := now.Add(time.Duration(i) * time.Hour)
		if !s.isPeakHour(nextHour.Hour()) {
			return nextHour
		}
	}

	// Fallback: 24 hours from now
	return now.Add(24 * time.Hour)
}

// RecordSegmentAccess records access to a segment for cold segment analysis
func (s *SmartCompactionScheduler) RecordSegmentAccess(segmentID int64) {
	if s.segmentAnalyzer != nil {
		s.segmentAnalyzer.RecordAccess(segmentID)
	}
}

// Stop cleans up resources
func (s *SmartCompactionScheduler) Stop() {
	if s.queryLoadMonitor != nil {
		s.queryLoadMonitor.Stop()
	}
}
