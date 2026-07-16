package worker

import (
	"context"
	"log/slog"
	"time"
)

var updateAppointmentTimeout = 5 * time.Second

type ExpireOverdueFunc func(ctx context.Context) (int64, error)

type Worker struct {
	logger         *slog.Logger
	expireOverdue  ExpireOverdueFunc
	tickerInterval time.Duration
}

func NewWorker(logger *slog.Logger, expireOverdue ExpireOverdueFunc, tickerInterval time.Duration) (*Worker, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if expireOverdue == nil {
		return nil, ErrNilAppointmentExpirer
	}
	if tickerInterval <= 0 {
		return nil, ErrInvalidTickerInterval
	}

	return &Worker{
		logger:         logger,
		expireOverdue:  expireOverdue,
		tickerInterval: tickerInterval,
	}, nil
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.processExpiredAppointments(ctx)
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "worker stopped")
			return
		}
	}
}

func (w *Worker) processExpiredAppointments(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, updateAppointmentTimeout)
	defer cancel()

	count, err := w.expireOverdue(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to expire overdue appointments", slog.Any("error", err))
		return
	}

	if count == 0 {
		w.logger.DebugContext(ctx, "no overdue appointments")
		return
	}

	w.logger.InfoContext(ctx, "expired overdue appointments", slog.Int64("count", count))
}
