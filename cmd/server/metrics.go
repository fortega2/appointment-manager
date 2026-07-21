package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"appointment-manager/internal/metrics"
)

const (
	metricsPath                  = "/metrics"
	metricsReadHeaderTimeout     = 5 * time.Second
	metricsServerShutdownTimeout = 3 * time.Second
)

// startMetricsServer runs the Prometheus metrics endpoint on its own listener,
// keeping it off the public app chain (CSRF/Gzip/auth) and off Caddy's proxy. It
// returns a stop func that gracefully shuts the server down; callers defer it so
// shutdown stays ordered ahead of the pool being closed.
func startMetricsServer(ctx context.Context, logger *slog.Logger, m *metrics.Metrics, addr string) func() {
	mux := http.NewServeMux()
	mux.Handle(metricsPath, m.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: metricsReadHeaderTimeout,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		logger.InfoContext(ctx, "metrics server listening", slog.String("addr", addr), slog.String("path", metricsPath))

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "metrics server error", slog.Any("error", err))
		}
	}()

	return func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), metricsServerShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutCtx); err != nil {
			logger.ErrorContext(ctx, "metrics server shutdown error", slog.Any("error", err))
		}

		<-done
		logger.InfoContext(ctx, "metrics server stopped")
	}
}
