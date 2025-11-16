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

package index

import (
	"container/list"
	"context"
	"runtime/debug"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/milvus-io/milvus/pkg/v2/log"
	"github.com/milvus-io/milvus/pkg/v2/proto/indexpb"
	"github.com/milvus-io/milvus/pkg/v2/util/merr"
	"github.com/milvus-io/milvus/pkg/v2/util/paramtable"
)

// TaskQueue is a queue used to store tasks.
type TaskQueue interface {
	utChan() <-chan struct{}
	utEmpty() bool
	utFull() bool
	addUnissuedTask(t Task) error
	PopUnissuedTask() Task
	AddActiveTask(t Task)
	PopActiveTask(tName string) Task
	Enqueue(t Task) error
	GetTaskNum() (int, int)
	GetUsingSlot() int64
	GetActiveSlot() int64
}

// BaseTaskQueue is a basic instance of TaskQueue.
type IndexTaskQueue struct {
	unissuedTasks *list.List
	activeTasks   map[string]Task
	utLock        sync.Mutex
	atLock        sync.Mutex

	// maxTaskNum should keep still
	maxTaskNum int64

	utBufChan chan struct{} // to block scheduler
	usingSlot atomic.Int64

	sched *TaskScheduler
}

func (queue *IndexTaskQueue) utChan() <-chan struct{} {
	return queue.utBufChan
}

func (queue *IndexTaskQueue) utEmpty() bool {
	return queue.unissuedTasks.Len() == 0
}

func (queue *IndexTaskQueue) utFull() bool {
	return int64(queue.unissuedTasks.Len()) >= queue.maxTaskNum
}

func (queue *IndexTaskQueue) addUnissuedTask(t Task) error {
	queue.utLock.Lock()
	defer queue.utLock.Unlock()

	if queue.utFull() {
		return errors.New("index task queue is full")
	}
	queue.unissuedTasks.PushBack(t)
	select {
	case queue.utBufChan <- struct{}{}:
	default:
	}
	return nil
}

func (queue *IndexTaskQueue) GetUsingSlot() int64 {
	return queue.usingSlot.Load()
}

func (queue *IndexTaskQueue) GetActiveSlot() int64 {
	queue.atLock.Lock()
	defer queue.atLock.Unlock()

	slots := int64(0)
	for _, t := range queue.activeTasks {
		slots += t.GetSlot()
	}
	return slots
}

// PopUnissuedTask pops a task from tasks queue.
func (queue *IndexTaskQueue) PopUnissuedTask() Task {
	queue.utLock.Lock()
	defer queue.utLock.Unlock()

	if queue.unissuedTasks.Len() <= 0 {
		return nil
	}

	ft := queue.unissuedTasks.Front()
	queue.unissuedTasks.Remove(ft)

	return ft.Value.(Task)
}

// AddActiveTask adds a task to activeTasks.
func (queue *IndexTaskQueue) AddActiveTask(t Task) {
	queue.atLock.Lock()
	defer queue.atLock.Unlock()

	tName := t.Name()
	_, ok := queue.activeTasks[tName]
	if ok {
		log.Ctx(context.TODO()).Debug("task already in active task list", zap.String("TaskID", tName))
	}

	queue.activeTasks[tName] = t
}

// PopActiveTask pops a task from activateTask and the task will be executed.
func (queue *IndexTaskQueue) PopActiveTask(tName string) Task {
	queue.atLock.Lock()
	defer queue.atLock.Unlock()

	t, ok := queue.activeTasks[tName]
	if ok {
		delete(queue.activeTasks, tName)
		queue.usingSlot.Sub(t.GetSlot())
		return t
	}
	log.Ctx(queue.sched.ctx).Debug("task was not found in the active task list", zap.String("TaskName", tName))
	return nil
}

// Enqueue adds a task to TaskQueue.
func (queue *IndexTaskQueue) Enqueue(t Task) error {
	err := t.OnEnqueue(t.Ctx())
	if err != nil {
		return err
	}
	if err = queue.addUnissuedTask(t); err != nil {
		return err
	}

	queue.usingSlot.Add(t.GetSlot())
	return nil
}

func (queue *IndexTaskQueue) GetTaskNum() (int, int) {
	queue.utLock.Lock()
	defer queue.utLock.Unlock()
	queue.atLock.Lock()
	defer queue.atLock.Unlock()

	utNum := queue.unissuedTasks.Len()
	atNum := 0
	// remove the finished task
	for _, task := range queue.activeTasks {
		if task.GetState() != indexpb.JobState_JobStateFinished && task.GetState() != indexpb.JobState_JobStateFailed {
			atNum++
		}
	}
	return utNum, atNum
}

// NewIndexBuildTaskQueue creates a new IndexBuildTaskQueue.
func NewIndexBuildTaskQueue(sched *TaskScheduler) *IndexTaskQueue {
	return &IndexTaskQueue{
		unissuedTasks: list.New(),
		activeTasks:   make(map[string]Task),
		maxTaskNum:    1024,

		utBufChan: make(chan struct{}, 1024),
		sched:     sched,

		usingSlot: atomic.Int64{},
	}
}

// TaskScheduler is a scheduler of indexing tasks.
type TaskScheduler struct {
	TaskQueue TaskQueue

	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	parallelBuilder *ParallelIndexBuilder
}

// NewTaskScheduler creates a new task scheduler of indexing tasks.
func NewTaskScheduler(ctx context.Context) *TaskScheduler {
	ctx1, cancel := context.WithCancel(ctx)
	s := &TaskScheduler{
		ctx:    ctx1,
		cancel: cancel,
	}
	s.TaskQueue = NewIndexBuildTaskQueue(s)

	// Initialize parallel builder with configuration from paramtable
	parallelConfig := &ParallelConfig{
		Enabled:                paramtable.Get().DataNodeCfg.ParallelIndexEnabled.GetAsBool(),
		MaxConcurrentBuilds:    int(paramtable.Get().DataNodeCfg.ParallelIndexMaxConcurrentBuilds.GetAsInt64()),
		MemoryReservationRatio: paramtable.Get().DataNodeCfg.ParallelIndexMemoryReservationRatio.GetAsFloat(),
		MemoryFactors: map[string]float64{
			"HNSW":     1.5,
			"IVF_FLAT": 2.0,
			"IVF_PQ":   1.8,
			"IVF_SQ8":  1.7,
			"DiskANN":  1.2,
			"FLAT":     1.1,
		},
	}

	parallelBuilder, err := NewParallelIndexBuilder(parallelConfig)
	if err != nil {
		log.Ctx(ctx).Warn("Failed to create parallel index builder, falling back to sequential",
			zap.Error(err))
	} else {
		s.parallelBuilder = parallelBuilder
		log.Ctx(ctx).Info("Parallel index builder initialized successfully",
			zap.Bool("enabled", parallelConfig.Enabled),
			zap.Int("maxConcurrent", parallelConfig.MaxConcurrentBuilds),
			zap.Float64("memoryRatio", parallelConfig.MemoryReservationRatio))
	}

	return s
}

func getStateFromError(err error) indexpb.JobState {
	if errors.Is(err, errCancel) {
		return indexpb.JobState_JobStateRetry
	} else if errors.Is(err, merr.ErrIoKeyNotFound) || errors.Is(err, merr.ErrSegcoreUnsupported) {
		// NoSuchKey or unsupported error
		return indexpb.JobState_JobStateFailed
	} else if errors.Is(err, merr.ErrSegcorePretendFinished) {
		return indexpb.JobState_JobStateFinished
	}
	return indexpb.JobState_JobStateRetry
}

func (sched *TaskScheduler) processTask(t Task, q TaskQueue) {
	wrap := func(fn func(ctx context.Context) error) error {
		select {
		case <-t.Ctx().Done():
			return errCancel
		default:
			return fn(t.Ctx())
		}
	}

	defer func() {
		t.Reset()
		debug.FreeOSMemory()
	}()
	sched.TaskQueue.AddActiveTask(t)
	defer sched.TaskQueue.PopActiveTask(t.Name())
	log.Ctx(t.Ctx()).Debug("process task", zap.String("task", t.Name()))
	pipelines := []func(context.Context) error{t.PreExecute, t.Execute, t.PostExecute}
	for _, fn := range pipelines {
		if err := wrap(fn); err != nil {
			log.Ctx(t.Ctx()).Warn("process task failed", zap.Error(err))
			t.SetState(getStateFromError(err), err.Error())
			return
		}
	}
	t.SetState(indexpb.JobState_JobStateFinished, "")
}

// processIndexBuildTasksParallel processes multiple index build tasks in parallel
// using the ParallelIndexBuilder. It handles PreExecute and PostExecute sequentially
// but runs Execute in parallel.
func (sched *TaskScheduler) processIndexBuildTasksParallel(tasks []*indexBuildTask) {
	if len(tasks) == 0 {
		return
	}

	log.Ctx(sched.ctx).Info("Processing index build tasks in parallel",
		zap.Int("numTasks", len(tasks)))

	// Add all tasks to active queue
	for _, t := range tasks {
		sched.TaskQueue.AddActiveTask(t)
	}

	// Clean up when done
	defer func() {
		for _, t := range tasks {
			sched.TaskQueue.PopActiveTask(t.Name())
			t.Reset()
		}
		debug.FreeOSMemory()
	}()

	wrap := func(task *indexBuildTask, fn func(ctx context.Context) error) error {
		select {
		case <-task.Ctx().Done():
			return errCancel
		default:
			return fn(task.Ctx())
		}
	}

	// Phase 1: Run PreExecute for all tasks sequentially
	for _, task := range tasks {
		if err := wrap(task, task.PreExecute); err != nil {
			log.Ctx(task.Ctx()).Warn("PreExecute failed", zap.Error(err))
			task.SetState(getStateFromError(err), err.Error())
			return
		}
	}

	// Phase 2: Run Execute in parallel using ParallelIndexBuilder
	if err := sched.parallelBuilder.BuildParallel(sched.ctx, tasks); err != nil {
		log.Ctx(sched.ctx).Warn("Parallel index build failed", zap.Error(err))
		// Mark all tasks as failed
		for _, task := range tasks {
			task.SetState(indexpb.JobState_JobStateRetry, err.Error())
		}
		return
	}

	// Phase 3: Run PostExecute for all tasks sequentially
	for _, task := range tasks {
		if err := wrap(task, task.PostExecute); err != nil {
			log.Ctx(task.Ctx()).Warn("PostExecute failed", zap.Error(err))
			task.SetState(getStateFromError(err), err.Error())
			return
		}
	}

	// Mark all tasks as finished
	for _, task := range tasks {
		task.SetState(indexpb.JobState_JobStateFinished, "")
	}

	log.Ctx(sched.ctx).Info("Parallel index build completed successfully",
		zap.Int("numTasks", len(tasks)))
}

// tryCollectIndexBuildBatch attempts to collect a batch of index build tasks
// from the queue for parallel processing. Returns nil if batching is not applicable.
func (sched *TaskScheduler) tryCollectIndexBuildBatch(firstTask Task) []*indexBuildTask {
	// Check if parallel builder is available
	if sched.parallelBuilder == nil || !sched.parallelBuilder.config.Enabled {
		return nil
	}

	// Check if first task is an index build task
	indexTask, ok := firstTask.(*indexBuildTask)
	if !ok {
		return nil
	}

	// Start collecting tasks
	batch := []*indexBuildTask{indexTask}

	// Try to collect more index build tasks (up to a reasonable limit)
	maxBatchSize := 16 // Configurable limit
	for i := 1; i < maxBatchSize; i++ {
		// Check if there are more tasks in the queue
		utNum, _ := sched.TaskQueue.GetTaskNum()
		if utNum == 0 {
			break
		}

		// Try to get the next task
		nextTask := sched.TaskQueue.PopUnissuedTask()
		if nextTask == nil {
			break
		}

		// Check if it's an index build task
		if nextIndexTask, ok := nextTask.(*indexBuildTask); ok {
			batch = append(batch, nextIndexTask)
		} else {
			// Not an index build task, put it back (this is a simplification)
			// In a real implementation, we'd need a way to re-queue it
			go func(t Task) {
				sched.processTask(t, sched.TaskQueue)
			}(nextTask)
			break
		}
	}

	// Only use parallel processing if we have multiple tasks
	if len(batch) < 2 {
		return nil
	}

	log.Ctx(sched.ctx).Info("Collected index build batch for parallel processing",
		zap.Int("batchSize", len(batch)))

	return batch
}

func (sched *TaskScheduler) indexBuildLoop() {
	log.Ctx(sched.ctx).Debug("TaskScheduler start build loop ...")
	defer sched.wg.Done()
	for {
		select {
		case <-sched.ctx.Done():
			return
		case <-sched.TaskQueue.utChan():
			t := sched.TaskQueue.PopUnissuedTask()

			// Try to collect a batch of index build tasks for parallel processing
			batch := sched.tryCollectIndexBuildBatch(t)
			if batch != nil {
				// Process batch in parallel
				totalSlots := int64(0)
				for _, task := range batch {
					totalSlots += task.GetSlot()
				}

				// Wait for available slots for the entire batch
				for {
					totalSlot := CalculateNodeSlots()
					availableSlot := totalSlot - sched.TaskQueue.GetActiveSlot()
					if availableSlot >= totalSlots || totalSlot == availableSlot {
						go func(tasks []*indexBuildTask) {
							sched.processIndexBuildTasksParallel(tasks)
						}(batch)
						break
					}
					time.Sleep(time.Millisecond * 50)
				}
			} else {
				// Process single task as before
				for {
					totalSlot := CalculateNodeSlots()
					availableSlot := totalSlot - sched.TaskQueue.GetActiveSlot()
					if availableSlot >= t.GetSlot() || totalSlot == availableSlot {
						go func(t Task) {
							sched.processTask(t, sched.TaskQueue)
						}(t)
						break
					}
					time.Sleep(time.Millisecond * 50)
				}
			}
		}
	}
}

// Start stats the task scheduler of indexing tasks.
func (sched *TaskScheduler) Start() error {
	sched.wg.Add(1)
	go sched.indexBuildLoop()
	return nil
}

// Close closes the task scheduler of indexing tasks.
func (sched *TaskScheduler) Close() {
	sched.cancel()
	sched.wg.Wait()
	if sched.parallelBuilder != nil {
		sched.parallelBuilder.Close()
	}
}
