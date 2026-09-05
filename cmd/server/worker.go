package main

import (
	"appointment-manager/internal/outbox"
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
	drainOutboxJobName       = "drain-outbox"
	workerJobTimeout         = 5 * time.Second
)

// workerDeps groups what the sweeps run against, so adding one costs no
// parameter on startBackgroundWorkers.
type workerDeps struct {
	deps            *dependencies
	sessionStore    *session.Store
	resetTokenStore *passwordreset.Store
	loginLimiter    *ratelimit.Limiter
	resetLimiter    *ratelimit.Limiter
	outboxRelay     *outbox.Relay
}

// startBackgroundWorkers runs periodic sweeps until stop is called, then waits
// for all workers to exit. Jobs own their rules; this only schedules them.
func startBackgroundWorkers(ctx context.Context, logger *slog.Logger, w workerDeps) (func(), error) {
	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
	if err != nil {
		return nil, err
	}

	outboxDrainInterval, err := parseOutboxDrainInterval(os.Getenv(outboxDrainIntervalEnv))
	if err != nil {
		return nil, err
	}

	group, err := worker.NewGroup(logger, worker.Config{
		Interval:   workerInterval,
		JobTimeout: workerJobTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create worker group: %w", err)
	}

	jobs := []struct {
		name string
		run  worker.JobFunc
		opts []worker.JobOption
	}{
		{name: expireOverdueJobName, run: w.deps.appointmentService.ExpireOverdue},
		{name: reconcileCancelsJobName, run: w.deps.appointmentService.CancelOnBlockedSlots},
		{name: expireSessionsJobName, run: w.sessionStore.DeleteExpired},
		{name: expireResetTokensJobName, run: w.resetTokenStore.DeleteExpired},
		{name: sweepLoginLimiterJobName, run: w.loginLimiter.DeleteExpired},
		{name: sweepResetLimiterJobName, run: w.resetLimiter.DeleteExpired},
		// Polls faster so outbox retries respect their backoff schedule.
		{name: drainOutboxJobName, run: w.outboxRelay.Drain, opts: []worker.JobOption{worker.WithInterval(outboxDrainInterval)}},
	}

	for _, job := range jobs {
		if err := group.Add(job.name, job.run, job.opts...); err != nil {
			return nil, fmt.Errorf("failed to register %s worker: %w", job.name, err)
		}
	}

	group.Start(ctx)

	return group.Stop, nil
}
