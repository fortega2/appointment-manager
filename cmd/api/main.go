package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/db"
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/password"
	"appointment-manager/internal/server"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	databaseURLEnv          = "DATABASE_URL"
	serverAddr              = ":8080"
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 10 * time.Second
	serverWriteTimeout      = 15 * time.Second
	serverIdleTimeout       = 60 * time.Second
	serverMaxHeaderBytes    = 1 << 20
	serverShutdownTimeout   = 3 * time.Second
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	logger.Info("starting API server")

	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		logger.Error("database URL is not set", slog.String("env", databaseURLEnv))
		return fmt.Errorf("%s is required", databaseURLEnv)
	}

	pool, err := db.NewPostgresPool(context.Background(), databaseURL)
	if err != nil {
		logger.Error("failed to initialize postgres pool", slog.Any("error", err))
		return err
	}
	defer pool.Close()

	assistantRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("failed to create assistant postgres repository", slog.Any("error", err))
		return err
	}

	passwordHasher := password.NewArgon2()
	assistantService, err := assistant.NewService(assistantRepo, passwordHasher)
	if err != nil {
		logger.Error("failed to create assistant service", slog.Any("error", err))
		return err
	}

	assistantHandler, err := assistant.NewHandler(logger, assistantService)
	if err != nil {
		logger.Error("failed to create assistant handler", slog.Any("error", err))
		return err
	}

	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("failed to create appointment postgres repository", slog.Any("error", err))
		return err
	}

	appointmentService, err := appointment.NewService(appointmentRepo)
	if err != nil {
		logger.Error("failed to create appointment service", slog.Any("error", err))
		return err
	}

	appointmentHandler, err := appointment.NewHandler(logger, appointmentService)
	if err != nil {
		logger.Error("failed to create appointment handler", slog.Any("error", err))
		return err
	}

	mux := http.NewServeMux()
	assistantHandler.RegisterHandlers(mux)
	appointmentHandler.RegisterHandlers(mux)
	handler := middleware.Chain(
		mux,
		middleware.RequestID(),
		middleware.Gzip(),
		middleware.RequestLogger(logger),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Start(ctx, logger, handler, serverAddr, server.Config{
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
		ShutdownTimeout:   serverShutdownTimeout,
	}); err != nil {
		logger.ErrorContext(ctx, "server error", slog.Any("error", err))
		return err
	}

	return nil
}
