package main

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/server"
	"appointment-manager/internal/session"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/joho/godotenv"
)

const (
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
	logLevel, err := parseLogLevel(os.Getenv(logLevelEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	logger.Info("starting server")

	if err := godotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			logger.Error("failed to load .env file", slog.Any("error", err))
			return err
		}
		logger.Debug(".env file not found, using OS environment variables")
	}

	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		logger.Error("database URL is not set")
		return fmt.Errorf("%s is required", databaseURLEnv)
	}

	pool, err := db.NewPostgresPool(context.Background(), databaseURL)
	if err != nil {
		logger.Error("failed to initialize postgres pool", slog.Any("error", err))
		return err
	}
	defer pool.Close()
	defer func(logger *slog.Logger) {
		logger.Info("postgres pool closed")
	}(logger)

	storageClient, err := initializeStorageClient(context.Background(), logger)
	if err != nil {
		return err
	}

	deps, err := newDependencies(pool)
	if err != nil {
		logger.Error("failed to initialize dependencies", slog.Any("error", err))
		return err
	}

	env := strings.TrimSpace(os.Getenv(environmentEnv))
	isDev := env == "" || strings.EqualFold(env, environmentDevelopment)

	sessionStore := session.NewStore()
	handler, err := initializeServerHandlers(logger, sessionStore, deps, storageClient, isDev)
	if err != nil {
		logger.Error("failed to initialize server handlers", slog.Any("error", err))
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stopWorker, err := startOverdueWorker(ctx, logger, deps)
	if err != nil {
		logger.ErrorContext(ctx, "failed to start overdue appointment worker", slog.Any("error", err))
		return err
	}
	defer stopWorker()

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
