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

	components, err := initializeAppComponents(logger, pool, appMetrics)
	if err != nil {
		logger.Error("failed to initialize application components", slog.Any("error", err))
		return err
	}

	handler, err := initializeServerHandlers(handlerConfig{
		logger:           logger,
		sessionStore:     components.sessionStore,
		deps:             components.deps,
		storageClient:    storageClient,
		metrics:          appMetrics,
		sendNotification: components.notificationService.NotifySlotCancelled,
		locale:           components.locale,
		isDev:            components.isDev,
	})
	if err != nil {
		logger.Error("failed to initialize server handlers", slog.Any("error", err))
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stopMetricsServer := startMetricsServer(ctx, logger, appMetrics, parseMetricsAddr(os.Getenv(metricsAddrEnv)))
	defer stopMetricsServer()

	stopNotificationWorker := startNotificationWorker(context.WithoutCancel(ctx), components.notificationService)
	defer stopNotificationWorker()

	stopWorkers, err := startBackgroundWorkers(ctx, logger, components.deps, components.sessionStore)
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
