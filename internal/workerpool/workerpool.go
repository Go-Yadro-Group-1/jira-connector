package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
			slog.Debug("workerpool: pool stopped due to error", "error", err)
		}

		// Close resultCh only after every worker has returned, so no worker can
		// send on a closed channel. Stop() (called by the producer) only closes
		// the input channel; ownership of closing the output stays here.
		wp.stopped.Store(true)
		close(wp.resultCh)
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

// Stop signals that no more tasks will be submitted by closing the input
// channel. It does NOT close the result channel — that is owned by Run's
// watcher goroutine and closed only after all workers have returned, which
// prevents a "send on closed channel" panic on in-flight results.
func (wp *WorkerPool) Stop() {
	wp.once.Do(func() {
		wp.stopped.Store(true)
		close(wp.taskCh)
	})
}

func (wp *WorkerPool) Stats() *PoolStats {
	return &wp.stats
}

func (wp *WorkerPool) worker(ctx context.Context, identifier int) error {
	slog.Debug("workerpool: worker started", "worker_id", identifier)

	for {
		select {
		case task, ok := <-wp.taskCh:
			if !ok {
				slog.Debug(
					"workerpool: worker stopping: task channel closed",
					"worker_id",
					identifier,
				)

				return nil
			}

			res, err := wp.processor.Process(ctx, task)
			result := NewTaskResult(task.ID, res, err)

			if err != nil {
				wp.stats.Failed.Add(1)
				slog.Debug(
					"workerpool: worker task failed",
					"worker_id", identifier,
					"task_id", task.ID,
					"error", err,
				)
			} else {
				wp.stats.Processed.Add(1)
			}

			select {
			case wp.resultCh <- result:
			case <-ctx.Done():
				slog.Debug(
					"workerpool: worker context done while sending result",
					"worker_id", identifier,
				)

				return fmt.Errorf("worker context cancelled: %w", ctx.Err())
			}

		case <-ctx.Done():
			slog.Debug("workerpool: worker stopping: context done", "worker_id", identifier)

			return fmt.Errorf("worker context cancelled: %w", ctx.Err())
		}
	}
}
