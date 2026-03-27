package main

import (
	"appointment-manager/internal/assistant"
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	serverAddr        = ":8080"
	readHeaderTimeout = 5 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	logger.Info("starting API server")

	assistantRepo := assistant.NewMemRepository()
	assistantHandler, err := assistant.NewHandler(logger, assistantRepo)
	if err != nil {
		logger.Error("failed to create assistant handler", slog.Any("error", err))
		os.Exit(1)
	}

	mux := http.NewServeMux()
	assistantHandler.RegisterHandlers(mux)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	srv := &http.Server{
		Addr:              serverAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("failed to start server", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	logger.Info("API server started on :8080")

	<-sig
	logger.Info("shutting down API server")

	if err := srv.Shutdown(context.Background()); err != nil {
		logger.Error("failed to shutdown server", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("API server stopped")
}
