package worker

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// JobFunc is a periodic sweep. It reports how many rows it changed so the
// worker can log the effect of a run, and zero is a valid, non-error result.
type JobFunc func(ctx context.Context) (int64, error)

// Config is the lifecycle policy a Group applies to the jobs it supervises.
// JobTimeout caps a single run, not the job's lifetime, so it must stay well
// under Interval or a slow run eats the next tick.
type Config struct {
	Interval   time.Duration
	JobTimeout time.Duration
}

// JobOption overrides the group defaults for one job.
type JobOption func(*job)

// WithInterval overrides Config.Interval for the job being added.
func WithInterval(interval time.Duration) JobOption {
	return func(j *job) { j.interval = interval }
}

type job struct {
	name     string
	run      JobFunc
	interval time.Duration
}

// Group supervises the process's periodic jobs: it owns their goroutines, the
// WaitGroup they are waited on, and the context that ends them, so a caller
// starts and stops every sweep as one unit. The work itself stays in the
// package it belongs to and reaches the group as a JobFunc.
type Group struct {
	logger *slog.Logger
	config Config
	jobs   []job

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// NewGroup builds an empty group. Both config durations are required.
func NewGroup(logger *slog.Logger, config Config) (*Group, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if config.Interval <= 0 {
		return nil, ErrInvalidTickerInterval
	}
	if config.JobTimeout <= 0 {
		return nil, ErrInvalidJobTimeout
	}

	return &Group{logger: logger, config: config}, nil
}

// Add registers a job under a name that identifies it in every log line, so
// jobs sharing the log stream stay distinguishable. Names must be unique, and a
// job cannot be added once the group started.
func (g *Group) Add(name string, run JobFunc, opts ...JobOption) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.started {
		return ErrGroupStarted
	}
	if strings.TrimSpace(name) == "" {
		return ErrEmptyJobName
	}
	if run == nil {
		return ErrNilJob
	}
	for _, existing := range g.jobs {
		if existing.name == name {
			return ErrDuplicateJobName
		}
	}

	added := job{name: name, run: run, interval: g.config.Interval}
	for _, opt := range opts {
		opt(&added)
	}
	if added.interval <= 0 {
		return ErrInvalidTickerInterval
	}

	g.jobs = append(g.jobs, added)

	return nil
}

// Start runs every registered job in its own goroutine until ctx ends or Stop
// is called. Starting an already started group does nothing.
func (g *Group) Start(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.started {
		return
	}
	g.started = true

	groupCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel

	for _, j := range g.jobs {
		g.wg.Go(func() { g.run(groupCtx, j) })

		g.logger.InfoContext(ctx, "worker started",
			slog.String("job", j.name), slog.Duration("interval", j.interval))
	}
}

// Stop ends every job and blocks until their goroutines have exited, so callers
// can defer it to keep shutdown ordered ahead of the resources the jobs use.
// One cancellation for the whole group drains the jobs in parallel.
func (g *Group) Stop() {
	g.mu.Lock()
	cancel := g.cancel
	g.cancel = nil
	g.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	g.wg.Wait()
}

func (g *Group) run(ctx context.Context, j job) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	defer func() {
		g.logger.InfoContext(ctx, "worker stopped", slog.String("job", j.name))
	}()

	select {
	case <-ctx.Done():
		return
	default:
		g.runOnce(ctx, j)
	}

	for {
		select {
		case <-ticker.C:
			g.runOnce(ctx, j)
		case <-ctx.Done():
			return
		}
	}
}

func (g *Group) runOnce(ctx context.Context, j job) {
	ctx, cancel := context.WithTimeout(ctx, g.config.JobTimeout)
	defer cancel()

	count, err := j.run(ctx)
	if err != nil {
		g.logger.ErrorContext(ctx, "worker job failed", slog.String("job", j.name), slog.Any("error", err))
		return
	}

	if count == 0 {
		g.logger.DebugContext(ctx, "worker job changed nothing", slog.String("job", j.name))
		return
	}

	g.logger.InfoContext(ctx, "worker job completed", slog.String("job", j.name), slog.Int64("count", count))
}
