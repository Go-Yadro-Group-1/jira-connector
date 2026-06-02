package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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
	logger      *slog.Logger

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
		logger:      slog.Default(),
	}
}

func (wp *WorkerPool) WithLogger(logger *slog.Logger) *WorkerPool {
	wp.logger = logger

	return wp
}

// Run starts all workers and returns a channel of results.
//
// The returned channel is closed exactly once — after all workers have finished
// their current tasks and exited. Callers must drain the channel to completion.
//
// taskCh must be closed by the producer (the caller of Submit) to signal that
// no more tasks will be submitted. Run does not close taskCh.
func (wp *WorkerPool) Run(ctx context.Context) <-chan TaskResult {
	var workerWG sync.WaitGroup

	for workerID := range wp.workerCount {
		workerWG.Add(1)

		go func(id int) {
			defer workerWG.Done()

			wp.worker(ctx, id)
		}(workerID)
	}

	// Close resultCh only after every worker has exited to prevent
	// "send on closed channel" panics.
	go func() {
		workerWG.Wait()
		wp.stopped.Store(true)
		close(wp.resultCh)
	}()

	return wp.resultCh
}

// Submit enqueues a task. Returns errPoolStopped if Stop has been called.
// If the context is cancelled before the task can be enqueued, returns a
// wrapped context error.
func (wp *WorkerPool) Submit(ctx context.Context, task Task) (retErr error) {
	if wp.stopped.Load() {
		return errPoolStopped
	}

	defer func() {
		if r := recover(); r != nil {
			retErr = errSubmitPanic
		}
	}()

	select {
	case wp.taskCh <- task:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("submit task: %w", ctx.Err())
	}
}

// Stop signals that no more tasks will be submitted by closing the input
// channel. It is idempotent; subsequent calls are no-ops.
// It does NOT close the result channel — that is owned by Run's
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

func (wp *WorkerPool) worker(ctx context.Context, identifier int) {
	wp.logger.InfoContext(ctx, "worker started", slog.Int("worker_id", identifier))

	for {
		select {
		case task, ok := <-wp.taskCh:
			if !ok {
				wp.logger.InfoContext(
					ctx,
					"worker stopping: task channel closed",
					slog.Int("worker_id", identifier),
				)

				return
			}

			res, err := wp.processor.Process(ctx, task)
			result := NewTaskResult(task.ID, res, err)

			if err != nil {
				wp.stats.Failed.Add(1)
				wp.logger.ErrorContext(
					ctx,
					"worker task failed",
					slog.Int("worker_id", identifier),
					slog.String("task_id", task.ID),
					slog.Any("error", err),
				)
			} else {
				wp.stats.Processed.Add(1)
			}

			select {
			case wp.resultCh <- result:
			case <-ctx.Done():
				wp.logger.InfoContext(
					ctx,
					"worker stopping: context done while sending result",
					slog.Int("worker_id", identifier),
				)

				return
			}

		case <-ctx.Done():
			wp.logger.InfoContext(
				ctx,
				"worker stopping: context done",
				slog.Int("worker_id", identifier),
			)

			return
		}
	}
}
