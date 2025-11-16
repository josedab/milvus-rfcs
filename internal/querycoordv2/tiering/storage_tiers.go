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
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
)

// Tier is the interface for storage tiers
type Tier interface {
	// GetTierType returns the type of storage tier
	GetTierType() StorageTier

	// LoadSegment loads a segment into this tier
	LoadSegment(ctx context.Context, segmentID int64, data []byte) error

	// UnloadSegment removes a segment from this tier
	UnloadSegment(ctx context.Context, segmentID int64) error

	// HasSegment checks if a segment exists in this tier
	HasSegment(segmentID int64) bool

	// GetSegmentSize returns the size of a segment in bytes
	GetSegmentSize(segmentID int64) (int64, error)

	// GetCapacity returns the total capacity in bytes
	GetCapacity() int64

	// GetUsedSpace returns the used space in bytes
	GetUsedSpace() int64

	// GetAvailableSpace returns available space in bytes
	GetAvailableSpace() int64
}

// MemoryTier represents the hot tier (in-memory storage)
type MemoryTier struct {
	mu           sync.RWMutex
	segments     map[int64][]byte
	maxCapacity  int64
	usedSpace    int64
	logger       *zap.Logger
	evictionFunc func(segmentID int64) // Callback for eviction
}

// NewMemoryTier creates a new memory tier
func NewMemoryTier(maxCapacityGB int64) *MemoryTier {
	return &MemoryTier{
		segments:    make(map[int64][]byte),
		maxCapacity: maxCapacityGB * 1024 * 1024 * 1024, // Convert GB to bytes
		usedSpace:   0,
		logger:      log.L(),
	}
}

func (mt *MemoryTier) GetTierType() StorageTier {
	return TierHot
}

func (mt *MemoryTier) LoadSegment(ctx context.Context, segmentID int64, data []byte) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	dataSize := int64(len(data))

	// Check if there's enough space
	if mt.usedSpace+dataSize > mt.maxCapacity {
		return fmt.Errorf("not enough space in memory tier: need %d bytes, available %d bytes",
			dataSize, mt.maxCapacity-mt.usedSpace)
	}

	// Store segment data
	mt.segments[segmentID] = data
	mt.usedSpace += dataSize

	mt.logger.Info("segment loaded to memory tier",
		zap.Int64("segmentID", segmentID),
		zap.Int64("sizeBytes", dataSize),
		zap.Int64("usedSpace", mt.usedSpace),
		zap.Int64("capacity", mt.maxCapacity))

	return nil
}

func (mt *MemoryTier) UnloadSegment(ctx context.Context, segmentID int64) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	data, exists := mt.segments[segmentID]
	if !exists {
		return fmt.Errorf("segment %d not found in memory tier", segmentID)
	}

	dataSize := int64(len(data))
	delete(mt.segments, segmentID)
	mt.usedSpace -= dataSize

	mt.logger.Info("segment unloaded from memory tier",
		zap.Int64("segmentID", segmentID),
		zap.Int64("sizeBytes", dataSize),
		zap.Int64("usedSpace", mt.usedSpace))

	return nil
}

func (mt *MemoryTier) HasSegment(segmentID int64) bool {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	_, exists := mt.segments[segmentID]
	return exists
}

func (mt *MemoryTier) GetSegmentSize(segmentID int64) (int64, error) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	data, exists := mt.segments[segmentID]
	if !exists {
		return 0, fmt.Errorf("segment %d not found in memory tier", segmentID)
	}

	return int64(len(data)), nil
}

func (mt *MemoryTier) GetCapacity() int64 {
	return mt.maxCapacity
}

func (mt *MemoryTier) GetUsedSpace() int64 {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.usedSpace
}

func (mt *MemoryTier) GetAvailableSpace() int64 {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.maxCapacity - mt.usedSpace
}

// SSDTier represents the warm tier (SSD storage)
type SSDTier struct {
	mu          sync.RWMutex
	segments    map[int64]int64 // segmentID -> size
	path        string
	maxCapacity int64
	usedSpace   int64
	logger      *zap.Logger
}

// NewSSDTier creates a new SSD tier
func NewSSDTier(path string, maxCapacityGB int64) *SSDTier {
	return &SSDTier{
		segments:    make(map[int64]int64),
		path:        path,
		maxCapacity: maxCapacityGB * 1024 * 1024 * 1024, // Convert GB to bytes
		usedSpace:   0,
		logger:      log.L(),
	}
}

func (st *SSDTier) GetTierType() StorageTier {
	return TierWarm
}

func (st *SSDTier) LoadSegment(ctx context.Context, segmentID int64, data []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	dataSize := int64(len(data))

	// Check if there's enough space
	if st.usedSpace+dataSize > st.maxCapacity {
		return fmt.Errorf("not enough space in SSD tier: need %d bytes, available %d bytes",
			dataSize, st.maxCapacity-st.usedSpace)
	}

	// In a real implementation, this would write to SSD
	// For now, we just track the size
	st.segments[segmentID] = dataSize
	st.usedSpace += dataSize

	st.logger.Info("segment loaded to SSD tier",
		zap.Int64("segmentID", segmentID),
		zap.Int64("sizeBytes", dataSize),
		zap.String("path", st.path))

	return nil
}

func (st *SSDTier) UnloadSegment(ctx context.Context, segmentID int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	size, exists := st.segments[segmentID]
	if !exists {
		return fmt.Errorf("segment %d not found in SSD tier", segmentID)
	}

	delete(st.segments, segmentID)
	st.usedSpace -= size

	st.logger.Info("segment unloaded from SSD tier",
		zap.Int64("segmentID", segmentID),
		zap.Int64("sizeBytes", size))

	return nil
}

func (st *SSDTier) HasSegment(segmentID int64) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	_, exists := st.segments[segmentID]
	return exists
}

func (st *SSDTier) GetSegmentSize(segmentID int64) (int64, error) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	size, exists := st.segments[segmentID]
	if !exists {
		return 0, fmt.Errorf("segment %d not found in SSD tier", segmentID)
	}

	return size, nil
}

func (st *SSDTier) GetCapacity() int64 {
	return st.maxCapacity
}

func (st *SSDTier) GetUsedSpace() int64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.usedSpace
}

func (st *SSDTier) GetAvailableSpace() int64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.maxCapacity - st.usedSpace
}

// ObjectStorageTier represents the cold tier (S3/MinIO)
type ObjectStorageTier struct {
	mu          sync.RWMutex
	segments    map[int64]int64 // segmentID -> size
	bucket      string
	endpoint    string
	maxCapacity int64
	usedSpace   int64
	logger      *zap.Logger
}

// NewObjectStorageTier creates a new object storage tier
func NewObjectStorageTier(bucket, endpoint string, maxCapacityGB int64) *ObjectStorageTier {
	return &ObjectStorageTier{
		segments:    make(map[int64]int64),
		bucket:      bucket,
		endpoint:    endpoint,
		maxCapacity: maxCapacityGB * 1024 * 1024 * 1024, // Convert GB to bytes
		usedSpace:   0,
		logger:      log.L(),
	}
}

func (ost *ObjectStorageTier) GetTierType() StorageTier {
	return TierCold
}

func (ost *ObjectStorageTier) LoadSegment(ctx context.Context, segmentID int64, data []byte) error {
	ost.mu.Lock()
	defer ost.mu.Unlock()

	dataSize := int64(len(data))

	// Object storage typically has much larger capacity
	// In a real implementation, this would upload to S3/MinIO
	ost.segments[segmentID] = dataSize
	ost.usedSpace += dataSize

	ost.logger.Info("segment loaded to object storage tier",
		zap.Int64("segmentID", segmentID),
		zap.Int64("sizeBytes", dataSize),
		zap.String("bucket", ost.bucket))

	return nil
}

func (ost *ObjectStorageTier) UnloadSegment(ctx context.Context, segmentID int64) error {
	ost.mu.Lock()
	defer ost.mu.Unlock()

	size, exists := ost.segments[segmentID]
	if !exists {
		return fmt.Errorf("segment %d not found in object storage tier", segmentID)
	}

	delete(ost.segments, segmentID)
	ost.usedSpace -= size

	ost.logger.Info("segment unloaded from object storage tier",
		zap.Int64("segmentID", segmentID),
		zap.Int64("sizeBytes", size))

	return nil
}

func (ost *ObjectStorageTier) HasSegment(segmentID int64) bool {
	ost.mu.RLock()
	defer ost.mu.RUnlock()
	_, exists := ost.segments[segmentID]
	return exists
}

func (ost *ObjectStorageTier) GetSegmentSize(segmentID int64) (int64, error) {
	ost.mu.RLock()
	defer ost.mu.RUnlock()

	size, exists := ost.segments[segmentID]
	if !exists {
		return 0, fmt.Errorf("segment %d not found in object storage tier", segmentID)
	}

	return size, nil
}

func (ost *ObjectStorageTier) GetCapacity() int64 {
	return ost.maxCapacity
}

func (ost *ObjectStorageTier) GetUsedSpace() int64 {
	ost.mu.RLock()
	defer ost.mu.RUnlock()
	return ost.usedSpace
}

func (ost *ObjectStorageTier) GetAvailableSpace() int64 {
	ost.mu.RLock()
	defer ost.mu.RUnlock()
	return ost.maxCapacity - ost.usedSpace
}
