package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"
)

const (
	pprofShutdownTimeout   = 5 * time.Second
	pprofReadHeaderTimeout = 30 * time.Second
)

// startPprofServer starts a dedicated HTTP server for pprof endpoints on addr.
// It does not block; the caller is responsible for shutdown via Server.Close().
func startPprofServer(addr string, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()

	// Register pprof handlers explicitly on our own mux instead of relying on
	// DefaultServeMux, so pprof is never accidentally exposed via other servers.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{ //nolint:exhaustruct
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: pprofReadHeaderTimeout,
	}

	go func() {
		logger.Info("pprof server listening", slog.String("addr", addr))

		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("pprof server exited", slog.Any("error", err))
		}
	}()

	return srv
}

// pprofShutdown gracefully stops the pprof server.
func pprofShutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), pprofShutdownTimeout)
	defer cancel()

	_ = srv.Shutdown(ctx)
}
