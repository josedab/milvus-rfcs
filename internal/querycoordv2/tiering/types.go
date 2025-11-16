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
	"time"
)

// StorageTier represents the tier of storage for a segment
type StorageTier int

const (
	// TierHot represents memory storage with <10ms latency
	TierHot StorageTier = iota
	// TierWarm represents SSD storage with <50ms latency
	TierWarm
	// TierCold represents object storage (S3/MinIO) with <500ms latency
	TierCold
)

// String returns the string representation of the storage tier
func (t StorageTier) String() string {
	switch t {
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	case TierCold:
		return "cold"
	default:
		return "unknown"
	}
}

// AccessStats tracks access statistics for a segment
type AccessStats struct {
	SegmentID     int64
	LastAccess    time.Time
	AccessCount   int64
	BytesRead     int64
	AvgLatency    time.Duration
	CurrentTier   StorageTier
	LastMigration time.Time
}

// TieringPolicy defines the policy for determining segment tiers
type TieringPolicy struct {
	// HotThreshold - not accessed within this duration triggers demotion from hot
	HotThreshold time.Duration
	// WarmThreshold - not accessed within this duration triggers demotion from warm
	WarmThreshold time.Duration
	// MinAccessCount - minimum access count required for hot tier
	MinAccessCount int64
	// HotAccessCountThreshold - access count threshold for promotion to hot
	HotAccessCountThreshold int64
	// HotMaxMemoryGB - maximum memory for hot tier in GB
	HotMaxMemoryGB int64
	// WarmMaxSizeGB - maximum size for warm tier in GB
	WarmMaxSizeGB int64
}

// DefaultTieringPolicy returns the default tiering policy
func DefaultTieringPolicy() *TieringPolicy {
	return &TieringPolicy{
		HotThreshold:            1 * time.Hour,
		WarmThreshold:           24 * time.Hour,
		MinAccessCount:          10,
		HotAccessCountThreshold: 100,
		HotMaxMemoryGB:          64,
		WarmMaxSizeGB:           512,
	}
}

// SegmentInfo represents basic segment information for tiering
type SegmentInfo struct {
	SegmentID    int64
	CollectionID int64
	PartitionID  int64
	SizeBytes    int64
	NodeID       int64
}

// MigrationTask represents a migration task from one tier to another
type MigrationTask struct {
	SegmentID  int64
	FromTier   StorageTier
	ToTier     StorageTier
	Priority   int
	CreateTime time.Time
	Status     MigrationStatus
}

// MigrationStatus represents the status of a migration task
type MigrationStatus int

const (
	// MigrationStatusPending indicates the migration is pending
	MigrationStatusPending MigrationStatus = iota
	// MigrationStatusRunning indicates the migration is in progress
	MigrationStatusRunning
	// MigrationStatusCompleted indicates the migration is completed
	MigrationStatusCompleted
	// MigrationStatusFailed indicates the migration failed
	MigrationStatusFailed
)

// String returns the string representation of migration status
func (s MigrationStatus) String() string {
	switch s {
	case MigrationStatusPending:
		return "pending"
	case MigrationStatusRunning:
		return "running"
	case MigrationStatusCompleted:
		return "completed"
	case MigrationStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}
