package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

var updateAppointmentTimeout = 5 * time.Second

// JobFunc is a periodic sweep. It reports how many rows it changed so the
// worker can log the effect of a run, and zero is a valid, non-error result.
type JobFunc func(ctx context.Context) (int64, error)

type Worker struct {
	logger         *slog.Logger
	job            JobFunc
	name           string
	tickerInterval time.Duration
}

// NewWorker builds a worker that runs one named job on an interval. The name
// identifies the job in every log line, so several workers can share the log
// stream without their runs becoming indistinguishable.
func NewWorker(logger *slog.Logger, name string, job JobFunc, tickerInterval time.Duration) (*Worker, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if strings.TrimSpace(name) == "" {
		return nil, ErrEmptyJobName
	}
	if job == nil {
		return nil, ErrNilJob
	}
	if tickerInterval <= 0 {
		return nil, ErrInvalidTickerInterval
	}

	return &Worker{
		logger:         logger,
		name:           name,
		job:            job,
		tickerInterval: tickerInterval,
	}, nil
}

// Run ticks until ctx ends. It logs nothing about its own lifecycle: whoever
// started it logs the pair, so the two lines cannot drift apart.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tickerInterval)
	defer ticker.Stop()

	select {
	case <-ctx.Done():
		return
	default:
		w.runJob(ctx)
	}

	for {
		select {
		case <-ticker.C:
			w.runJob(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (w *Worker) runJob(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, updateAppointmentTimeout)
	defer cancel()

	count, err := w.job(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "worker job failed", slog.String("job", w.name), slog.Any("error", err))
		return
	}

	if count == 0 {
		w.logger.DebugContext(ctx, "worker job changed nothing", slog.String("job", w.name))
		return
	}

	w.logger.InfoContext(ctx, "worker job completed", slog.String("job", w.name), slog.Int64("count", count))
}
