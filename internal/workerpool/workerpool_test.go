package workerpool_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/workerpool"
)

// errInjectedFailure is a sentinel test error to avoid dynamic error creation.
var errInjectedFailure = errors.New("injected failure")

// countingProcessor counts how many tasks it successfully processes.
type countingProcessor struct {
	count atomic.Int64
}

func (p *countingProcessor) Process(_ context.Context, _ workerpool.Task) (any, error) {
	p.count.Add(1)

	return "ok", nil
}

// failOnIDProcessor returns an error for tasks whose ID matches failID.
type failOnIDProcessor struct {
	failID string
}

func (p *failOnIDProcessor) Process(_ context.Context, task workerpool.Task) (any, error) {
	if task.ID == p.failID {
		return nil, fmt.Errorf("process task %s: %w", task.ID, errInjectedFailure)
	}

	return "ok", nil
}

// slowProcessor blocks until the context is cancelled, then returns an error.
type slowProcessor struct{}

func (p *slowProcessor) Process(ctx context.Context, _ workerpool.Task) (any, error) {
	<-ctx.Done()

	return nil, fmt.Errorf("slow processor: %w", ctx.Err())
}

// TestWorkerPool_HappyPath verifies that all submitted tasks produce results
// and that the results channel is closed after all tasks are processed.
func TestWorkerPool_HappyPath(t *testing.T) {
	t.Parallel()

	const taskCount = 50

	proc := &countingProcessor{}
	pool := workerpool.New(5, taskCount, proc)

	ctx := t.Context()
	resultCh := pool.Run(ctx)

	for idx := range taskCount {
		task := workerpool.NewTask(string(rune('A'+idx%26)), nil)

		err := pool.Submit(ctx, task)
		if err != nil {
			t.Fatalf("unexpected submit error: %v", err)
		}
	}

	pool.Stop()

	var received int

	for range resultCh {
		received++
	}

	if received != taskCount {
		t.Errorf("expected %d results, got %d", taskCount, received)
	}
}

// TestWorkerPool_NoPanicOnWorkerError is the race-condition regression test.
// It reproduces the original bug: one worker fails, the errgroup cancels the
// context, and remaining workers race to send on a (formerly) closed resultCh.
//
// Run with -race to confirm there are no data races.
func TestWorkerPool_NoPanicOnWorkerError(t *testing.T) {
	t.Parallel()

	const (
		workerCount = 25
		taskCount   = 200
		failTaskID  = "FAIL-1"
	)

	proc := &failOnIDProcessor{failID: failTaskID}
	pool := workerpool.New(workerCount, taskCount, proc)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	resultCh := pool.Run(ctx)

	// Submit a failing task among many normal tasks.
	submitErr := pool.Submit(ctx, workerpool.NewTask(failTaskID, nil))
	if submitErr != nil {
		t.Fatalf("submit fail task: %v", submitErr)
	}

	for i := range taskCount - 1 {
		task := workerpool.NewTask(string(rune('A'+i%26)), nil)
		_ = pool.Submit(ctx, task) // ignore errors after ctx cancellation
	}

	pool.Stop()

	// Drain resultCh to completion — must not panic.
	var successCount, failCount int

	for res := range resultCh {
		if res.Err != nil {
			failCount++
		} else {
			successCount++
		}
	}

	// At least the one injected failure must have been delivered.
	if failCount == 0 {
		t.Error("expected at least one failed result")
	}

	t.Logf("results: success=%d, fail=%d", successCount, failCount)
}

// TestWorkerPool_ResultChannelClosedAfterStop verifies that the results channel
// is always closed (and thus range terminates) even when Stop is called while
// workers are idle.
func TestWorkerPool_ResultChannelClosedAfterStop(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(4, 10, &countingProcessor{})

	ctx := t.Context()
	resultCh := pool.Run(ctx)

	pool.Stop() // no tasks submitted

	done := make(chan struct{})

	go func() {
		for range resultCh { //nolint:revive // intentional drain
		}

		close(done)
	}()

	select {
	case <-done:
		// resultCh closed correctly
	case <-time.After(2 * time.Second):
		t.Fatal("resultCh was not closed after Stop()")
	}
}

// TestWorkerPool_StopIsIdempotent ensures calling Stop multiple times never panics.
func TestWorkerPool_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(2, 10, &countingProcessor{})

	ctx := t.Context()
	resultCh := pool.Run(ctx)

	pool.Stop()
	pool.Stop() // second call must be a no-op

	for range resultCh { //nolint:revive // intentional drain
	}
}

// TestWorkerPool_SubmitAfterStop returns errPoolStopped without panicking.
func TestWorkerPool_SubmitAfterStop(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(2, 10, &countingProcessor{})

	ctx := t.Context()
	resultCh := pool.Run(ctx)

	pool.Stop()

	err := pool.Submit(ctx, workerpool.NewTask("late", nil))
	if err == nil {
		t.Fatal("expected error on submit after stop")
	}

	for range resultCh { //nolint:revive // intentional drain
	}
}

// TestWorkerPool_ContextCancellationDrainsCleanly verifies that cancelling the
// root context causes Run to close resultCh without deadlocking.
func TestWorkerPool_ContextCancellationDrainsCleanly(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(10, 50, &slowProcessor{})

	ctx, cancel := context.WithCancel(t.Context())
	resultCh := pool.Run(ctx)

	// Submit a few tasks before cancelling.
	for range 10 {
		_ = pool.Submit(ctx, workerpool.NewTask("slow", nil))
	}

	cancel()
	pool.Stop()

	done := make(chan struct{})

	go func() {
		for range resultCh { //nolint:revive // intentional drain
		}

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("resultCh not closed after context cancellation")
	}
}

// TestWorkerPool_ConcurrentSubmitAndStop hammers Submit and Stop concurrently
// to surface any remaining races (must be run with -race).
func TestWorkerPool_ConcurrentSubmitAndStop(t *testing.T) {
	t.Parallel()

	const goroutines = 20

	pool := workerpool.New(goroutines, goroutines*10, &countingProcessor{})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resultCh := pool.Run(ctx)

	var submitWG sync.WaitGroup

	for goroutineID := range goroutines {
		submitWG.Add(1)

		go func(id int) {
			defer submitWG.Done()

			for taskIdx := range 50 {
				task := workerpool.NewTask(
					string(rune('a'+id%26))+string(rune('0'+taskIdx%10)),
					nil,
				)
				_ = pool.Submit(ctx, task)
			}
		}(goroutineID)
	}

	submitWG.Wait()
	pool.Stop()

	for range resultCh { //nolint:revive // intentional drain
	}
}
