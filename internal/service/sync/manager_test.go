package sync_test

import (
	"sync"
	"testing"
	"time"

	syncsvc "github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

func TestManager_StartNewJob(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")

	if !res.Started {
		t.Fatal("expected Started=true for a new job")
	}

	if res.SyncID == "" {
		t.Fatal("expected non-empty SyncID")
	}
}

func TestManager_StartDeduplicatesRunningJob(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	first := manager.Start("PROJ")
	second := manager.Start("PROJ")

	if second.Started {
		t.Fatal("expected Started=false for duplicate running job")
	}

	if second.SyncID != first.SyncID {
		t.Fatalf("expected same SyncID, got %q and %q", first.SyncID, second.SyncID)
	}
}

func TestManager_StatusReturnsSnapshot(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")

	snap, ok := manager.Status(res.SyncID)

	if !ok {
		t.Fatal("expected ok=true for registered job")
	}

	if snap.State != syncsvc.JobStateRunning {
		t.Fatalf("expected Running, got %v", snap.State)
	}

	if snap.ProjectKey != "PROJ" {
		t.Fatalf("expected ProjectKey=PROJ, got %q", snap.ProjectKey)
	}
}

func TestManager_StatusUnknownID(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	_, ok := manager.Status("nonexistent")

	if ok {
		t.Fatal("expected ok=false for unknown sync_id")
	}
}

func TestManager_CompleteTransition(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")
	manager.Complete(res.SyncID)

	snap, ok := manager.Status(res.SyncID)

	if !ok {
		t.Fatal("expected ok=true after complete")
	}

	if snap.State != syncsvc.JobStateCompleted {
		t.Fatalf("expected Completed, got %v", snap.State)
	}

	if snap.FinishedAt.IsZero() {
		t.Fatal("expected non-zero FinishedAt")
	}
}

func TestManager_FailTransition(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")
	manager.Fail(res.SyncID, "connection refused")

	snap, ok := manager.Status(res.SyncID)

	if !ok {
		t.Fatal("expected ok=true after fail")
	}

	if snap.State != syncsvc.JobStateFailed {
		t.Fatalf("expected Failed, got %v", snap.State)
	}

	if snap.ErrMsg != "connection refused" {
		t.Fatalf("expected error message 'connection refused', got %q", snap.ErrMsg)
	}
}

func TestManager_StartNewJobAfterCompletion(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	first := manager.Start("PROJ")
	manager.Complete(first.SyncID)

	second := manager.Start("PROJ")

	if !second.Started {
		t.Fatal("expected Started=true for new job after previous completed")
	}

	if second.SyncID == first.SyncID {
		t.Fatal("expected different SyncID for new job")
	}
}

func TestManager_StartNewJobAfterFailure(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	first := manager.Start("PROJ")
	manager.Fail(first.SyncID, "oops")

	second := manager.Start("PROJ")

	if !second.Started {
		t.Fatal("expected Started=true for new job after previous failed")
	}

	if second.SyncID == first.SyncID {
		t.Fatal("expected different SyncID for new job")
	}
}

func TestManager_IncrProcessedAndSetTotal(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")
	manager.SetTotal(res.SyncID, 100)
	manager.IncrProcessed(res.SyncID)
	manager.IncrProcessed(res.SyncID)

	snap, _ := manager.Status(res.SyncID)

	if snap.Total != 100 {
		t.Fatalf("expected Total=100, got %d", snap.Total)
	}

	if snap.Processed != 2 {
		t.Fatalf("expected Processed=2, got %d", snap.Processed)
	}
}

func TestManager_ConcurrentStartsAreSafe(t *testing.T) {
	t.Parallel()

	manager := syncsvc.NewManager()

	const goroutines = 50

	var waitGroup sync.WaitGroup

	results := make([]syncsvc.StartResult, goroutines)

	for idx := range goroutines {
		waitGroup.Add(1)

		go func(pos int) {
			defer waitGroup.Done()

			results[pos] = manager.Start("PROJ")
		}(idx)
	}

	waitGroup.Wait()

	startedCount := 0

	var sharedID string

	for _, result := range results {
		if result.Started {
			startedCount++
			sharedID = result.SyncID
		}
	}

	if startedCount != 1 {
		t.Fatalf("expected exactly 1 goroutine to start the job, got %d", startedCount)
	}

	// All non-starters must return the same ID.
	for _, result := range results {
		if result.SyncID != sharedID {
			t.Fatalf(
				"expected all goroutines to see the same SyncID %q, got %q",
				sharedID,
				result.SyncID,
			)
		}
	}
}

func TestManager_TTLEviction(t *testing.T) {
	t.Parallel()

	// We cannot easily change the TTL constant in tests without dependency
	// injection. Instead, verify the eviction logic path: after Fail the
	// job is visible; a subsequent Start for any *other* key will trigger
	// evictExpiredLocked with the real TTL — the failed job stays because
	// TTL hasn't elapsed. This test just validates the Status still works
	// immediately after failure (not evicted prematurely).

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")
	manager.Fail(res.SyncID, "err")

	// Trigger eviction logic via Start for a different key.
	_ = manager.Start("OTHER")

	snap, ok := manager.Status(res.SyncID)

	if !ok {
		t.Fatal("expected job to still be present before TTL elapses")
	}

	if snap.State != syncsvc.JobStateFailed {
		t.Fatalf("expected Failed, got %v", snap.State)
	}
}

func TestManager_StartTimestampIsSet(t *testing.T) {
	t.Parallel()

	before := time.Now()

	manager := syncsvc.NewManager()

	res := manager.Start("PROJ")

	after := time.Now()

	snap, _ := manager.Status(res.SyncID)

	if snap.StartedAt.Before(before) || snap.StartedAt.After(after) {
		t.Fatalf(
			"StartedAt %v is outside expected range [%v, %v]",
			snap.StartedAt,
			before,
			after,
		)
	}
}
