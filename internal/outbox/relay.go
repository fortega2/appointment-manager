package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	selectPendingQuery = `
		SELECT
			id,
			aggregate_id,
			event_type,
			payload,
			attempts
		FROM
			public.outbox
		WHERE
			processed_at IS NULL
			AND available_at <= CURRENT_TIMESTAMP
		ORDER BY
			available_at, id
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`

	markProcessedQuery = `
		UPDATE public.outbox
		SET processed_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`

	markFailedQuery = `
		UPDATE public.outbox
		SET attempts = attempts + 1, available_at = $2, last_error = $3
		WHERE id = $1
	`

	backoffBase = time.Second
	backoffCap  = 5 * time.Minute
	backoffRate = 2
)

// Handler delivers one outbox event.
type Handler func(ctx context.Context, aggregateID uuid.UUID, payload json.RawMessage) error

// Relay drains public.outbox and dispatches rows to registered handlers.
type Relay struct {
	logger    *slog.Logger
	pool      *pgxpool.Pool
	handlers  map[EventType]Handler
	batchSize int
}

// NewRelay builds a Relay that claims up to batchSize rows per Drain call.
func NewRelay(logger *slog.Logger, pool *pgxpool.Pool, batchSize int) (*Relay, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if pool == nil {
		return nil, ErrNilPool
	}
	if batchSize <= 0 {
		return nil, ErrInvalidBatchSize
	}

	return &Relay{
		logger:    logger,
		pool:      pool,
		handlers:  make(map[EventType]Handler),
		batchSize: batchSize,
	}, nil
}

// Register binds a Handler to an EventType.
func (r *Relay) Register(eventType EventType, handler Handler) error {
	if eventType == "" {
		return ErrEmptyEventType
	}
	if handler == nil {
		return ErrNilHandler
	}
	if _, exists := r.handlers[eventType]; exists {
		return ErrDuplicateHandler
	}

	r.handlers[eventType] = handler

	return nil
}

// Drain claims and dispatches one batch of due events, returning the delivered count.
// SKIP LOCKED allows multiple relay instances to process different rows. Each row
// uses a savepoint so one failed delivery does not roll back the rest of the batch.
func (r *Relay) Drain(ctx context.Context) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin outbox drain transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	rows, err := r.claimPending(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("claim pending outbox events: %w", err)
	}

	var delivered int64
	for _, row := range rows {
		if err := r.handleRow(ctx, tx, row); err != nil {
			r.logger.ErrorContext(
				ctx, "outbox event delivery failed",
				slog.Int64("id", row.id),
				slog.String("aggregate_id", row.aggregateID.String()),
				slog.String("event_type", string(row.eventType)),
				slog.Any("error", err),
			)
			continue
		}
		delivered++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit outbox drain transaction: %w", err)
	}

	return delivered, nil
}

type pendingRow struct {
	payload     json.RawMessage
	eventType   EventType
	aggregateID uuid.UUID
	id          int64
	attempts    int16
}

func (r *Relay) claimPending(ctx context.Context, tx pgx.Tx) ([]pendingRow, error) {
	rows, err := tx.Query(ctx, selectPendingQuery, r.batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []pendingRow
	for rows.Next() {
		var row pendingRow
		if err := rows.Scan(&row.id, &row.aggregateID, &row.eventType, &row.payload, &row.attempts); err != nil {
			return nil, err
		}

		pending = append(pending, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pending, nil
}

// handleRow dispatches one row inside a savepoint.
func (r *Relay) handleRow(ctx context.Context, tx pgx.Tx, row pendingRow) error {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin outbox row savepoint: %w", err)
	}

	handler, ok := r.handlers[row.eventType]
	if !ok {
		_ = savepoint.Rollback(ctx)
		return r.markFailed(ctx, tx, row, ErrNoHandlerRegistered)
	}

	if err := handler(ctx, row.aggregateID, row.payload); err != nil {
		_ = savepoint.Rollback(ctx)
		return r.markFailed(ctx, tx, row, err)
	}

	if _, err := savepoint.Exec(ctx, markProcessedQuery, row.id); err != nil {
		_ = savepoint.Rollback(ctx)
		return r.markFailed(ctx, tx, row, err)
	}

	return savepoint.Commit(ctx)
}

func (r *Relay) markFailed(ctx context.Context, tx pgx.Tx, row pendingRow, cause error) error {
	availableAt := time.Now().Add(backoff(row.attempts))
	if _, err := tx.Exec(ctx, markFailedQuery, row.id, availableAt, cause.Error()); err != nil {
		return fmt.Errorf("mark outbox event failed: %w", err)
	}

	return cause
}

// backoff grows exponentially with the attempts already made and is capped.
func backoff(attempts int16) time.Duration {
	delay := backoffBase * time.Duration(math.Pow(backoffRate, float64(attempts)))
	if delay > backoffCap {
		return backoffCap
	}

	return delay
}
