package main

import (
	"appointment-manager/internal/worker"
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const expireOverdueJobName = "expire-overdue-appointments"

// startBackgroundWorkers runs the periodic appointment sweeps in the background
// until the returned stop func is called. stop cancels every worker and blocks
// until their goroutines have exited, so callers can defer it to keep shutdown
// ordered ahead of the pool being closed.
//
// Every job is a service method: the sweeps carry business rules and business
// metrics, so they stay behind the service rather than being assembled here.
func startBackgroundWorkers(ctx context.Context, logger *slog.Logger, deps *dependencies) (func(), error) {
	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
	if err != nil {
		return nil, err
	}

	// Each sweep is an independent job: one lagging or failing must not hold up
	// the others, so each gets its own ticker.
	jobs := []struct {
		name string
		run  worker.JobFunc
	}{
		{name: expireOverdueJobName, run: deps.appointmentService.ExpireOverdue},
	}

	stops := make([]func(), 0, len(jobs))
	stopAll := func() {
		for _, stop := range stops {
			stop()
		}
	}

	for _, job := range jobs {
		stop, err := startWorker(ctx, logger, job.name, job.run, workerInterval)
		if err != nil {
			stopAll()

			return nil, err
		}

		stops = append(stops, stop)
	}

	return stopAll, nil
}

func startWorker(
	ctx context.Context,
	logger *slog.Logger,
	name string,
	job worker.JobFunc,
	interval time.Duration,
) (func(), error) {
	w, err := worker.NewWorker(logger, name, job, interval)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s worker: %w", name, err)
	}

	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		w.Run(workerCtx)
	}()
	logger.InfoContext(ctx, "worker started", slog.String("job", name), slog.Duration("interval", interval))

	return func() {
		cancelWorker()
		<-workerDone
		logger.InfoContext(ctx, "worker stopped", slog.String("job", name))
	}, nil
}
