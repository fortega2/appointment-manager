package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type Config struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

func (c Config) validate() error {
	if c.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("invalid read header timeout: %w", ErrInvalidConfig)
	}
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("invalid read timeout: %w", ErrInvalidConfig)
	}
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("invalid write timeout: %w", ErrInvalidConfig)
	}
	if c.IdleTimeout <= 0 {
		return fmt.Errorf("invalid idle timeout: %w", ErrInvalidConfig)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("invalid shutdown timeout: %w", ErrInvalidConfig)
	}
	if c.MaxHeaderBytes <= 0 {
		return fmt.Errorf("invalid max header bytes: %w", ErrInvalidConfig)
	}
	return nil
}

func Start(ctx context.Context, logger *slog.Logger, handler http.Handler, addr string, cfg Config) error {
	if ctx == nil {
		return ErrNilContext
	}
	if addr == "" {
		return ErrEmptyAddress
	}
	if logger == nil {
		return ErrNilLogger
	}
	if handler == nil {
		return ErrNilHandler
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		logger.InfoContext(ctx, "api server listening", slog.String("addr", addr))

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen and serve: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.InfoContext(ctx, "shutdown signal received, shutting down HTTP server")
		shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return shutdown(shutCtx, srv, logger)
	}
}

func shutdown(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	logger.InfoContext(ctx, "HTTP server shutdown complete")
	return nil
}
