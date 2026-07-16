package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/worker"
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// startOverdueWorker builds the overdue-appointment worker and runs it in the
// background until the returned stop func is called. stop cancels the worker
// and blocks until its goroutine has exited, so callers can defer it to keep
// shutdown ordered ahead of the pool being closed.
func startOverdueWorker(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) (func(), error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize appointment repository", slog.Any("error", err))
		return nil, err
	}

	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
	if err != nil {
		logger.ErrorContext(ctx, "invalid worker ticker interval", slog.Any("error", err))
		return nil, err
	}

	overdueWorker, err := worker.NewWorker(logger, appointmentRepo.ExpireOverdue, workerInterval)
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize overdue appointment worker", slog.Any("error", err))
		return nil, err
	}

	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		overdueWorker.Run(workerCtx)
	}()
	logger.InfoContext(ctx, "overdue appointment worker started", slog.Duration("interval", workerInterval))

	return func() {
		cancelWorker()
		<-workerDone
		logger.InfoContext(ctx, "overdue appointment worker stopped")
	}, nil
}
