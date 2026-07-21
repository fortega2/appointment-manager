package main

import (
	"appointment-manager/internal/metrics"
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
func startOverdueWorker(ctx context.Context, logger *slog.Logger, deps *dependencies, m *metrics.Metrics) (func(), error) {
	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
	if err != nil {
		return nil, err
	}

	overdueWorker, err := worker.NewWorker(logger, recordedExpireOverdue(deps.appointmentRepo.ExpireOverdue, m), workerInterval)
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

// recordedExpireOverdue wraps the repository's ExpireOverdue so the number of
// appointments swept to absent is recorded as a business metric on each run.
func recordedExpireOverdue(expireOverdue worker.ExpireOverdueFunc, m *metrics.Metrics) worker.ExpireOverdueFunc {
	return func(ctx context.Context) (int64, error) {
		count, err := expireOverdue(ctx)
		if err == nil && count > 0 {
			m.RecordAppointmentsExpired(count)
		}

		return count, err
	}
}
