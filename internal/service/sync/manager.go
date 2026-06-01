package sync

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// jobTTL is how long a terminal (completed/failed) job is kept in the registry
// so callers can poll the final status before it is evicted.
const jobTTL = 10 * time.Minute

// JobState mirrors the proto SyncState enum values.
// Exported so the handler can map it to the proto enum without importing manager internals.
type JobState int32

const (
	JobStateRunning   JobState = 1
	JobStateCompleted JobState = 2
	JobStateFailed    JobState = 3
)

// job holds the mutable state of a single sync operation.
// Counters use atomics so the background goroutine can update them without
// holding the manager lock.
type job struct {
	id         string
	projectKey string
	state      atomic.Int32 // stores JobState values
	processed  atomic.Uint64
	total      atomic.Uint64
	errMsg     string    // set once, under Manager.mu, when job fails
	startedAt  time.Time // immutable after creation
	finishedAt time.Time // set under Manager.mu on terminal transition
}

// JobSnapshot is a value-copy of job fields safe to return to callers.
type JobSnapshot struct {
	ID         string
	ProjectKey string
	State      JobState
	Processed  uint64
	Total      uint64
	ErrMsg     string
	StartedAt  time.Time
	FinishedAt time.Time
}

func (j *job) snapshot() JobSnapshot {
	return JobSnapshot{
		ID:         j.id,
		ProjectKey: j.projectKey,
		State:      JobState(j.state.Load()),
		Processed:  j.processed.Load(),
		Total:      j.total.Load(),
		ErrMsg:     j.errMsg,
		StartedAt:  j.startedAt,
		FinishedAt: j.finishedAt,
	}
}

// Manager is the in-memory registry of sync jobs.
// It is the single source of truth for deduplication (one running job per
// project key) and for status polling.
type Manager struct {
	mu    sync.Mutex
	byID  map[string]*job
	byKey map[string]*job // only Running jobs
}

// NewManager creates an initialised Manager.
func NewManager() *Manager {
	return &Manager{
		byID:  make(map[string]*job),
		byKey: make(map[string]*job),
	}
}

// StartResult is returned by Manager.Start.
type StartResult struct {
	SyncID  string
	Started bool // false means a job for this key was already running
}

// Start either registers a new job for projectKey and returns Started=true,
// or returns the existing running job's ID with Started=false.
// It is the caller's responsibility to launch the actual work goroutine when
// Started=true.
func (m *Manager) Start(projectKey string) StartResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.evictExpiredLocked()

	if existing, ok := m.byKey[projectKey]; ok {
		return StartResult{SyncID: existing.id, Started: false}
	}

	newJob := &job{
		id:         uuid.NewString(),
		projectKey: projectKey,
		startedAt:  time.Now(),
	}

	newJob.state.Store(int32(JobStateRunning))

	m.byID[newJob.id] = newJob
	m.byKey[projectKey] = newJob

	return StartResult{SyncID: newJob.id, Started: true}
}

// Status returns a snapshot for the given sync_id.
// ok=false means the id is unknown (never existed or already evicted).
func (m *Manager) Status(syncID string) (JobSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	found, ok := m.byID[syncID]
	if !ok {
		return JobSnapshot{}, false
	}

	return found.snapshot(), true
}

// SetTotal updates the expected total issue count for a running job.
// Safe to call from the sync goroutine without holding the manager lock.
func (m *Manager) SetTotal(syncID string, total uint64) {
	m.mu.Lock()
	found, ok := m.byID[syncID]
	m.mu.Unlock()

	if ok {
		found.total.Store(total)
	}
}

// IncrProcessed atomically increments the processed counter for a running job.
// Safe to call from the sync goroutine without holding the manager lock.
func (m *Manager) IncrProcessed(syncID string) {
	m.mu.Lock()
	found, ok := m.byID[syncID]
	m.mu.Unlock()

	if ok {
		found.processed.Add(1)
	}
}

// Complete transitions the job to Completed state.
func (m *Manager) Complete(syncID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	found, ok := m.byID[syncID]
	if !ok {
		return
	}

	found.state.Store(int32(JobStateCompleted))
	found.finishedAt = time.Now()

	delete(m.byKey, found.projectKey)
}

// Fail transitions the job to Failed state and records the error message.
func (m *Manager) Fail(syncID string, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	found, ok := m.byID[syncID]
	if !ok {
		return
	}

	found.state.Store(int32(JobStateFailed))
	found.errMsg = errMsg
	found.finishedAt = time.Now()

	delete(m.byKey, found.projectKey)
}

// evictExpiredLocked removes terminal jobs whose TTL has elapsed.
// Must be called with m.mu held.
func (m *Manager) evictExpiredLocked() {
	now := time.Now()

	for syncID, candidate := range m.byID {
		state := JobState(candidate.state.Load())
		if state == JobStateRunning {
			continue
		}

		if now.Sub(candidate.finishedAt) >= jobTTL {
			delete(m.byID, syncID)
		}
	}
}
