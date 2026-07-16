package main

import (
	"appointment-manager/internal/worker"
	"context"
	"fmt"
	"log/slog"
	"os"
)

// startOverdueWorker builds the overdue-appointment worker and runs it in the
// background until the returned stop func is called. stop cancels the worker
// and blocks until its goroutine has exited, so callers can defer it to keep
// shutdown ordered ahead of the pool being closed.
func startOverdueWorker(ctx context.Context, logger *slog.Logger, deps *dependencies) (func(), error) {
	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
	if err != nil {
		return nil, err
	}

	overdueWorker, err := worker.NewWorker(logger, deps.appointmentRepo.ExpireOverdue, workerInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to create overdue appointment worker: %w", err)
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
