package main

import (
	"appointment-manager/internal/passwordreset"
	"appointment-manager/internal/ratelimit"
	"appointment-manager/internal/session"
	"appointment-manager/internal/worker"
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	expireOverdueJobName     = "expire-overdue-appointments"
	reconcileCancelsJobName  = "cancel-appointments-on-blocked-slots"
	expireSessionsJobName    = "expire-sessions"
	expireResetTokensJobName = "expire-password-reset-tokens"
	sweepLoginLimiterJobName = "sweep-login-rate-limiter"
	sweepResetLimiterJobName = "sweep-password-reset-rate-limiter"
)

// workerDeps groups what the sweeps run against, so adding one does not grow
// startBackgroundWorkers another positional parameter.
type workerDeps struct {
	deps            *dependencies
	sessionStore    *session.Store
	resetTokenStore *passwordreset.Store
	loginLimiter    *ratelimit.Limiter
	resetLimiter    *ratelimit.Limiter
}

// startBackgroundWorkers runs the periodic appointment sweeps in the background
// until the returned stop func is called. stop cancels every worker and blocks
// until their goroutines have exited, so callers can defer it to keep shutdown
// ordered ahead of the pool being closed.
//
// Every job is a method on the thing it sweeps: the work carries rules and
// metrics that belong to its own package rather than being assembled here.
func startBackgroundWorkers(ctx context.Context, logger *slog.Logger, w workerDeps) (func(), error) {
	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
	if err != nil {
		return nil, err
	}

	jobs := []struct {
		name string
		run  worker.JobFunc
	}{
		{name: expireOverdueJobName, run: w.deps.appointmentService.ExpireOverdue},
		{name: reconcileCancelsJobName, run: w.deps.appointmentService.CancelOnBlockedSlots},
		{name: expireSessionsJobName, run: w.sessionStore.DeleteExpired},
		{name: expireResetTokensJobName, run: w.resetTokenStore.DeleteExpired},
		{name: sweepLoginLimiterJobName, run: w.loginLimiter.DeleteExpired},
		{name: sweepResetLimiterJobName, run: w.resetLimiter.DeleteExpired},
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
