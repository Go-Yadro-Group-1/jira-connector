package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

var (
	errPoolStopped = errors.New("worker pool is stopped")
	errSubmitPanic = errors.New("submit task: panic recovered")
)

type Task struct {
	ID      string
	Payload any
}

func NewTask(id string, payload any) Task {
	return Task{
		ID:      id,
		Payload: payload,
	}
}

func (t Task) GetID() string {
	return t.ID
}

func (t Task) GetPayload() any {
	return t.Payload
}

type TaskResult struct {
	TaskID string
	Result any
	Err    error
}

func NewTaskResult(taskID string, result any, err error) TaskResult {
	return TaskResult{
		TaskID: taskID,
		Result: result,
		Err:    err,
	}
}

func (tr TaskResult) GetTaskID() string {
	return tr.TaskID
}

func (tr TaskResult) GetErr() error {
	return tr.Err
}

func (tr TaskResult) IsSuccess() bool {
	return tr.Err == nil
}

type WorkerPool struct {
	workerCount int
	taskCh      chan Task
	resultCh    chan TaskResult
	processor   TaskProcessor
	stats       PoolStats

	once    sync.Once
	stopped atomic.Bool
}

type PoolStats struct {
	Processed atomic.Uint64
	Failed    atomic.Uint64
}

type TaskProcessor interface {
	Process(ctx context.Context, task Task) (any, error)
}

func New(workerCount int, queueSize int, processor TaskProcessor) *WorkerPool {
	return &WorkerPool{
		workerCount: workerCount,
		taskCh:      make(chan Task, queueSize),
		resultCh:    make(chan TaskResult, queueSize),
		processor:   processor,
	}
}

func (wp *WorkerPool) Run(ctx context.Context) <-chan TaskResult {
	errGroup, ctx := errgroup.WithContext(ctx)

	for i := range wp.workerCount {
		errGroup.Go(func() error {
			return wp.worker(ctx, i)
		})
	}

	go func() {
		err := errGroup.Wait()
		if err != nil {
			log.Printf("[workerpool] Worker pool stopped due to error: %v", err)
		}

		wp.Stop()
	}()

	return wp.resultCh
}

func (wp *WorkerPool) Submit(ctx context.Context, task Task) error {
	if wp.stopped.Load() {
		return errPoolStopped
	}

	var panicked bool

	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()

	select {
	case wp.taskCh <- task:
		if panicked {
			return errSubmitPanic
		}

		return nil
	case <-ctx.Done():
		return fmt.Errorf("submit task: %w", ctx.Err())
	}
}

func (wp *WorkerPool) Stop() {
	wp.once.Do(func() {
		wp.stopped.Store(true)
		close(wp.taskCh)
		close(wp.resultCh)
	})
}

func (wp *WorkerPool) Stats() *PoolStats {
	return &wp.stats
}

func (wp *WorkerPool) worker(ctx context.Context, identifier int) error {
	log.Printf("[workerpool] Worker %d started", identifier)

	for {
		select {
		case task, ok := <-wp.taskCh:
			if !ok {
				log.Printf(
					"[workerpool] Worker %d stopping: task channel closed",
					identifier,
				)

				return nil
			}

			res, err := wp.processor.Process(ctx, task)
			result := NewTaskResult(task.ID, res, err)

			if err != nil {
				wp.stats.Failed.Add(1)
				log.Printf(
					"[workerpool] Worker %d: task %s failed: %v",
					identifier,
					task.ID,
					err,
				)
			} else {
				wp.stats.Processed.Add(1)
			}

			select {
			case wp.resultCh <- result:
			case <-ctx.Done():
				log.Printf(
					"[workerpool] Worker %d: context done while sending result",
					identifier,
				)

				return fmt.Errorf("worker context cancelled: %w", ctx.Err())
			}

		case <-ctx.Done():
			log.Printf(
				"[workerpool] Worker %d stopping: context done",
				identifier,
			)

			return fmt.Errorf("worker context cancelled: %w", ctx.Err())
		}
	}
}
