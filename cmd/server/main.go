package main

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/server"
	"appointment-manager/internal/tracing"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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

	// Clears auth.dispatchTimeout so an in-flight mail finishes, and stays under
	// the stop_grace_period in docker/docker-compose.yml so SIGKILL never wins.
	resetDrainTimeout = 50 * time.Second
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

	logger := slog.New(tracing.NewSlogHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))
	logger.Info("starting server")

	if err := i18n.Load(); err != nil {
		logger.Error("failed to load locale catalogs", slog.Any("error", err))
		return err
	}

	appMetrics := metrics.New()

	if err := godotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			logger.Error("failed to load .env file", slog.Any("error", err))
			return err
		}
		logger.Debug(".env file not found, using OS environment variables")
	}

	stopTracing, err := startTracing(context.Background(), logger)
	if err != nil {
		logger.Error("failed to initialize tracing", slog.Any("error", err))
		return err
	}
	defer stopTracing()

	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		logger.Error("database URL is not set")
		return fmt.Errorf("%s is required", databaseURLEnv)
	}

	poolOptions, err := parsePoolConfig(poolConfig{
		MaxConns:              os.Getenv(dbPoolMaxConnsEnv),
		MinConns:              os.Getenv(dbPoolMinConnsEnv),
		MaxConnLifetime:       os.Getenv(dbPoolMaxConnLifetimeEnv),
		MaxConnLifetimeJitter: os.Getenv(dbPoolMaxConnLifetimeJitterEnv),
		MaxConnIdleTime:       os.Getenv(dbPoolMaxConnIdleTimeEnv),
		HealthCheckPeriod:     os.Getenv(dbPoolHealthCheckPeriodEnv),
	})
	if err != nil {
		logger.Error("failed to parse database pool configuration", slog.Any("error", err))
		return err
	}

	poolOptions = append(poolOptions, db.WithQueryTracer(appMetrics.DBTracer()))
	pool, err := db.NewPostgresPool(context.Background(), databaseURL, poolOptions...)
	if err != nil {
		logger.Error("failed to initialize postgres pool", slog.Any("error", err))
		return err
	}
	defer pool.Close()
	defer func(logger *slog.Logger) {
		logger.Info("postgres pool closed")
	}(logger)

	logPoolConfig(logger, pool.Config())

	appMetrics.RegisterDBPool(pool)

	storageClient, err := initializeStorageClient(context.Background(), logger)
	if err != nil {
		return err
	}

	components, err := initializeAppComponents(context.Background(), logger, pool, appMetrics)
	if err != nil {
		logger.Error("failed to initialize application components", slog.Any("error", err))
		return err
	}

	handler, shutdownFunc, err := initializeServerHandlers(handlerConfig{
		logger:           logger,
		components:       components,
		storageClient:    storageClient,
		metrics:          appMetrics,
		sendNotification: components.notificationService.NotifySlotCancelled,
	})
	if err != nil {
		logger.Error("failed to initialize server handlers", slog.Any("error", err))
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stopMetricsServer := startMetricsServer(ctx, logger, appMetrics, parseMetricsAddr(os.Getenv(metricsAddrEnv)))
	defer stopMetricsServer()

	// Detached from ctx on purpose: sharing it would end the drain the moment
	// SIGTERM arrives, while the server is still finishing in-flight requests, so
	// a slot cancelled in those last seconds would be lost silently. Deferred
	// here so it stops after the sweeps but before the pool closes -- the final
	// flush still queries for recipients.
	stopNotificationWorker := startNotificationWorker(context.WithoutCancel(ctx), components.notificationService)
	defer stopNotificationWorker()

	// Deferred before the workers so it runs after them and before the pool
	// closes: a reset mail still in flight queries for the account it is for.
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resetDrainTimeout)
		defer cancel()

		start := time.Now()

		err := shutdownFunc(ctx)
		if err == nil {
			logger.InfoContext(ctx, "password reset dispatches drained")
			return
		}

		logger.WarnContext(ctx, "password reset dispatches abandoned at shutdown",
			slog.Duration("waited", time.Since(start)), slog.Any("error", err))
	}()

	stopWorkers, err := startBackgroundWorkers(ctx, logger, workerDeps{
		deps:            components.deps,
		sessionStore:    components.sessionStore,
		resetTokenStore: components.resetTokenStore,
		loginLimiter:    components.loginLimiter,
		resetLimiter:    components.resetLimiter,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to start background workers", slog.Any("error", err))
		return err
	}
	defer stopWorkers()

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
