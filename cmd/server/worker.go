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
	workerJobTimeout         = 5 * time.Second
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

// startBackgroundWorkers runs the periodic sweeps in the background until the
// returned stop func is called. stop cancels every worker and blocks until
// their goroutines have exited, so callers can defer it to keep shutdown
// ordered ahead of the pool being closed.
//
// Every job is a method on the thing it sweeps: the work carries rules and
// metrics that belong to its own package rather than being assembled here, and
// the worker group owns nothing but when it runs.
func startBackgroundWorkers(ctx context.Context, logger *slog.Logger, w workerDeps) (func(), error) {
	workerInterval, err := parseWorkerInterval(os.Getenv(workerIntervalEnv))
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
	}{
		{name: expireOverdueJobName, run: w.deps.appointmentService.ExpireOverdue},
		{name: reconcileCancelsJobName, run: w.deps.appointmentService.CancelOnBlockedSlots},
		{name: expireSessionsJobName, run: w.sessionStore.DeleteExpired},
		{name: expireResetTokensJobName, run: w.resetTokenStore.DeleteExpired},
		{name: sweepLoginLimiterJobName, run: w.loginLimiter.DeleteExpired},
		{name: sweepResetLimiterJobName, run: w.resetLimiter.DeleteExpired},
	}

	for _, job := range jobs {
		if err := group.Add(job.name, job.run); err != nil {
			return nil, fmt.Errorf("failed to register %s worker: %w", job.name, err)
		}
	}

	group.Start(ctx)

	return group.Stop, nil
}
