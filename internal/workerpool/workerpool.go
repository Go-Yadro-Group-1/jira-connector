package workerpool

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
)

type Task struct {
	ID      string
	Payload any
}

type TaskResult struct {
	TaskID string
	Err    error
}

type WorkerPool struct {
	workerCount int
	taskCh      chan Task
	resultCh    chan TaskResult
	wg          sync.WaitGroup
	processor   TaskProcessor
	stats       PoolStats
}

type PoolStats struct {
	Processed atomic.Int64
	Failed    atomic.Int64
}

type TaskProcessor interface {
	Process(ctx context.Context, task Task) error
}

type IssueTaskPayload struct {
	IssueKey  string
	ProjectID int64
}

func New(workerCount int, queueSize int, processor TaskProcessor) *WorkerPool {
	return &WorkerPool{
		workerCount: workerCount,
		taskCh:      make(chan Task, queueSize),
		resultCh:    make(chan TaskResult, queueSize),
		processor:   processor,
	}
}

func (wp *WorkerPool) Run(ctx context.Context) (chan<- Task, <-chan TaskResult) {
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.worker(ctx, i)
	}

	go func() {
		wp.wg.Wait()
		close(wp.resultCh)
	}()

	return wp.taskCh, wp.resultCh
}

func (wp *WorkerPool) Submit(ctx context.Context, task Task) error {
	select {
	case wp.taskCh <- task:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("submit task: %w", ctx.Err())
	}
}

func (wp *WorkerPool) Stop() {
	close(wp.taskCh)
	wp.wg.Wait()
}

func (wp *WorkerPool) Stats() PoolStats {
	return wp.stats
}

func (wp *WorkerPool) worker(ctx context.Context, id int) {
	defer wp.wg.Done()

	log.Printf("[workerpool] Worker %d started", id)

	for {
		select {
		case task, ok := <-wp.taskCh:
			if !ok {
				log.Printf("[workerpool] Worker %d stopping: task channel closed", id)
				return
			}

			err := wp.processor.Process(ctx, task)

			result := TaskResult{
				TaskID: task.ID,
				Err:    err,
			}

			if err != nil {
				wp.stats.Failed.Add(1)
				log.Printf("[workerpool] Worker %d: task %s failed: %v", id, task.ID, err)
			} else {
				wp.stats.Processed.Add(1)
			}

			select {
			case wp.resultCh <- result:
			case <-ctx.Done():
				log.Printf("[workerpool] Worker %d: context done while sending result", id)
				return
			}

		case <-ctx.Done():
			log.Printf("[workerpool] Worker %d stopping: context done", id)
			return
		}
	}
}
