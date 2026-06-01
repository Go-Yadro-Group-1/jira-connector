// Package metrics holds the Prometheus instruments for the connector and the
// single registry they are registered against. A nil *Metrics is safe to pass
// around: the recording helpers become no-ops, so the CLI sync path (which has
// no scrape endpoint) does not need to build a registry.
package metrics

import (
	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const namespace = "connector"

// Outcome label values for the Jira client.
const (
	OutcomeSuccess = "success"
	OutcomeError   = "error"
)

// Metrics bundles every Prometheus instrument plus the registry that backs the
// /metrics endpoint and the gRPC server interceptor.
type Metrics struct {
	Registry *prometheus.Registry

	// GRPCServer instruments every unary RPC (count, latency, gRPC code).
	GRPCServer *grpcprom.ServerMetrics

	// Jira client instruments.
	JiraRequests        *prometheus.CounterVec
	JiraRequestDuration *prometheus.HistogramVec
	JiraRetries         prometheus.Counter

	// Sync job instruments.
	SyncStarted         prometheus.Counter
	SyncCompleted       prometheus.Counter
	SyncFailed          prometheus.Counter
	SyncActive          prometheus.Gauge
	SyncIssuesProcessed prometheus.Counter
}

// New builds the instruments and registers them against a fresh registry.
func New() *Metrics {
	mtr := &Metrics{ //nolint:exhaustruct
		Registry:   prometheus.NewRegistry(),
		GRPCServer: grpcprom.NewServerMetrics(grpcprom.WithServerHandlingTimeHistogram()),
	}

	mtr.initJira()
	mtr.initSync()

	mtr.Registry.MustRegister(
		mtr.JiraRequests,
		mtr.JiraRequestDuration,
		mtr.JiraRetries,
		mtr.SyncStarted,
		mtr.SyncCompleted,
		mtr.SyncFailed,
		mtr.SyncActive,
		mtr.SyncIssuesProcessed,
		mtr.GRPCServer,
	)

	return mtr
}

// RegisterRuntimeCollectors adds the standard Go runtime and process collectors.
func (m *Metrics) RegisterRuntimeCollectors() {
	m.Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), //nolint:exhaustruct
	)
}

// ObserveJiraRequest records the duration and outcome of a single Jira operation.
// It is a no-op on a nil receiver so callers need no nil checks.
func (m *Metrics) ObserveJiraRequest(operation string, seconds float64, err error) {
	if m == nil {
		return
	}

	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeError
	}

	m.JiraRequests.WithLabelValues(operation, outcome).Inc()
	m.JiraRequestDuration.WithLabelValues(operation).Observe(seconds)
}

// IncJiraRetry counts one Jira request retry. No-op on a nil receiver.
func (m *Metrics) IncJiraRetry() {
	if m == nil {
		return
	}

	m.JiraRetries.Inc()
}

// SyncJobStarted records the start of a sync job. No-op on a nil receiver.
func (m *Metrics) SyncJobStarted() {
	if m == nil {
		return
	}

	m.SyncStarted.Inc()
	m.SyncActive.Inc()
}

// SyncJobFinished records a terminal sync transition. No-op on a nil receiver.
func (m *Metrics) SyncJobFinished(failed bool) {
	if m == nil {
		return
	}

	if failed {
		m.SyncFailed.Inc()
	} else {
		m.SyncCompleted.Inc()
	}

	m.SyncActive.Dec()
}

// IncIssuesProcessed counts one persisted issue. No-op on a nil receiver.
func (m *Metrics) IncIssuesProcessed() {
	if m == nil {
		return
	}

	m.SyncIssuesProcessed.Inc()
}

func (m *Metrics) initJira() {
	m.JiraRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "jira",
			Name:      "requests_total",
			Help:      "Number of Jira REST requests by operation and outcome.",
		},
		[]string{"operation", "outcome"},
	)

	m.JiraRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "jira",
			Name:      "request_duration_seconds",
			Help:      "Duration of Jira REST requests by operation.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	m.JiraRetries = prometheus.NewCounter(
		prometheus.CounterOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "jira",
			Name:      "retries_total",
			Help:      "Number of Jira REST request retries (rate limit, 5xx, network errors).",
		},
	)
}

func (m *Metrics) initSync() {
	m.SyncStarted = prometheus.NewCounter(
		prometheus.CounterOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "sync",
			Name:      "jobs_started_total",
			Help:      "Number of sync jobs started.",
		},
	)

	m.SyncCompleted = prometheus.NewCounter(
		prometheus.CounterOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "sync",
			Name:      "jobs_completed_total",
			Help:      "Number of sync jobs that finished successfully.",
		},
	)

	m.SyncFailed = prometheus.NewCounter(
		prometheus.CounterOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "sync",
			Name:      "jobs_failed_total",
			Help:      "Number of sync jobs that failed.",
		},
	)

	m.SyncActive = prometheus.NewGauge(
		prometheus.GaugeOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "sync",
			Name:      "jobs_active",
			Help:      "Number of sync jobs currently running.",
		},
	)

	m.SyncIssuesProcessed = prometheus.NewCounter(
		prometheus.CounterOpts{ //nolint:exhaustruct
			Namespace: namespace,
			Subsystem: "sync",
			Name:      "issues_processed_total",
			Help:      "Number of issues persisted across all sync jobs.",
		},
	)
}
