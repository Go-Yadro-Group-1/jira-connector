package metrics_test

import (
	"errors"
	"testing"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

var errBoom = errors.New("boom")

func TestNewRegistersInstruments(t *testing.T) {
	t.Parallel()

	mtr := metrics.New()
	mtr.RegisterRuntimeCollectors()

	mtr.ObserveJiraRequest("get_issue", 0.1, nil)
	mtr.ObserveJiraRequest("get_issue", 0.2, errBoom)
	mtr.IncJiraRetry()
	mtr.SyncJobStarted()
	mtr.IncIssuesProcessed()
	mtr.ObserveSyncDuration(1.5)
	mtr.SyncJobFinished(false)

	got := testutil.ToFloat64(
		mtr.JiraRequests.WithLabelValues("get_issue", metrics.OutcomeSuccess),
	)
	if got != 1 {
		t.Fatalf("jira success counter = %v, want 1", got)
	}

	if active := testutil.ToFloat64(mtr.SyncActive); active != 0 {
		t.Fatalf("sync active gauge = %v, want 0 after finish", active)
	}

	if processed := testutil.ToFloat64(mtr.SyncIssuesProcessed); processed != 1 {
		t.Fatalf("issues processed = %v, want 1", processed)
	}
}

// TestNilMetricsAreNoOps guards the CLI sync path, which passes a nil *Metrics.
func TestNilMetricsAreNoOps(t *testing.T) {
	t.Parallel()

	var mtr *metrics.Metrics

	// None of these must panic on a nil receiver.
	mtr.ObserveJiraRequest("get_issue", 0.1, nil)
	mtr.IncJiraRetry()
	mtr.SyncJobStarted()
	mtr.SyncJobFinished(true)
	mtr.IncIssuesProcessed()
	mtr.ObserveSyncDuration(2.0)
}

func TestSyncDurationIsRecorded(t *testing.T) {
	t.Parallel()

	mtr := metrics.New()

	mtr.ObserveSyncDuration(3.5)

	// Gather the metric family and verify at least one observation was recorded.
	metricFamilies, err := mtr.Registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	const wantName = "connector_sync_duration_seconds"

	var found bool

	for _, mf := range metricFamilies {
		if mf.GetName() == wantName {
			found = true

			ms := mf.GetMetric()
			if len(ms) == 0 || ms[0].GetHistogram().GetSampleCount() != 1 {
				t.Fatalf("%s: want sample_count=1, got %v", wantName, ms)
			}

			break
		}
	}

	if !found {
		t.Fatalf("metric %q not found in registry", wantName)
	}
}
