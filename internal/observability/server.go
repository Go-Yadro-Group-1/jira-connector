// Package observability wires the connector's diagnostic HTTP endpoints:
// Prometheus metrics scraping and net/http/pprof profiling. Each endpoint runs
// on its own HTTP server so they can be bound to different ports (or disabled)
// independently of the gRPC server.
package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// Server owns the optional metrics and pprof HTTP servers and shuts them down
// together.
type Server struct {
	servers []*http.Server
	log     *slog.Logger
}

// New starts the enabled diagnostic servers in the background and returns a
// handle for graceful shutdown. Metrics server is only started when mtr is
// non-nil and metricsAddr is non-empty. Pprof server is only started when
// pprofAddr is non-empty.
func New(
	logger *slog.Logger,
	mtr *metrics.Metrics,
	metricsAddr string,
	pprofAddr string,
) *Server {
	srv := &Server{
		servers: nil,
		log:     logger,
	}

	if mtr != nil && metricsAddr != "" {
		mux := http.NewServeMux()
		handlerOpts := promhttp.HandlerOpts{Registry: mtr.Registry}
		mux.Handle("/metrics", promhttp.HandlerFor(mtr.Registry, handlerOpts))
		srv.start("metrics", metricsAddr, mux)
	}

	if pprofAddr != "" {
		// Register pprof handlers explicitly on our own mux instead of relying on
		// DefaultServeMux, so pprof is never accidentally exposed via other servers.
		srv.start("pprof", pprofAddr, pprofMux())
	}

	return srv
}

// pprofMux registers the standard net/http/pprof handlers on a dedicated mux.
func pprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return mux
}

// Shutdown gracefully stops every diagnostic server.
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for _, httpSrv := range s.servers {
		err := httpSrv.Shutdown(ctx)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("observability server shutdown error", slog.Any("error", err))
		}
	}
}

func (s *Server) start(name, addr string, handler http.Handler) {
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	s.servers = append(s.servers, httpSrv)

	go func() {
		s.log.Info(
			"diagnostic server listening",
			slog.String("name", name),
			slog.String("addr", addr),
		)

		err := httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error(
				"diagnostic server failed",
				slog.String("name", name),
				slog.Any("error", err),
			)
		}
	}()
}
