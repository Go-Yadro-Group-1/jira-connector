// Package observability wires the connector's diagnostic HTTP endpoints:
// Prometheus metrics scraping and net/http/pprof profiling. Each endpoint runs
// on its own HTTP server so they can be bound to different ports (or disabled)
// independently of the gRPC server.
package observability

import (
	"context"
	"errors"
	"log"
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
	log     *log.Logger
}

// New starts the enabled diagnostic servers in the background and returns a
// handle for graceful shutdown. A nil *metrics.Metrics disables the metrics
// endpoint even if metricsAddr is set.
func New(
	logger *log.Logger,
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
		handlerOpts := promhttp.HandlerOpts{Registry: mtr.Registry} //nolint:exhaustruct
		mux.Handle("/metrics", promhttp.HandlerFor(mtr.Registry, handlerOpts))
		srv.start("metrics", metricsAddr, mux)
	}

	if pprofAddr != "" {
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
			s.log.Printf("observability server shutdown: %v", err)
		}
	}
}

func (s *Server) start(name, addr string, handler http.Handler) {
	httpSrv := &http.Server{ //nolint:exhaustruct
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	s.servers = append(s.servers, httpSrv)

	go func() {
		s.log.Printf("%s server listening on %s", name, addr)

		err := httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Printf("%s server failed: %v", name, err)
		}
	}()
}
