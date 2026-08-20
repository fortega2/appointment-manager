package session

import (
	"appointment-manager/internal/domain"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	constraintFkSessionAssistant = "fk_session_assistant"

	createErrMsg            = "create session: %w"
	getErrMsg               = "get session: %w"
	deleteErrMsg            = "delete session: %w"
	deleteByAssistantErrMsg = "delete assistant sessions: %w"
	deleteExpiredErrMsg     = "delete expired sessions: %w"

	createSessionQuery = `
		INSERT INTO public.session (
			id,
			assistant_id,
			created_at,
			expires_at
		) VALUES ($1, $2, $3, $4)
	`

	getSessionQuery = `
		SELECT
			id,
			assistant_id,
			created_at,
			expires_at
		FROM
			public.session
		WHERE
			id = $1
	`

	deleteSessionQuery = `
		DELETE FROM
			public.session
		WHERE
			id = $1
	`

	deleteAssistantSessionsQuery = `
		DELETE FROM
			public.session
		WHERE
			assistant_id = $1
	`

	deleteExpiredSessionsQuery = `
		DELETE FROM
			public.session
		WHERE
			expires_at < $1
	`
)

// PostgresRepository persists sessions. Every id it receives is already a token
// digest; it never sees the token itself.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &PostgresRepository{pool: pool}, nil
}

func (r *PostgresRepository) Create(ctx context.Context, s Session) error {
	assistantID, err := domain.ParseID(s.UserID)
	if err != nil {
		return fmt.Errorf(createErrMsg, fmt.Errorf("%w: %w", ErrInvalidAssistantID, err))
	}

	if _, err := r.pool.Exec(ctx, createSessionQuery,
		s.ID,
		assistantID,
		s.CreatedAt,
		s.ExpiresAt,
	); err != nil {
		return mapPgxError(createErrMsg, err)
	}

	return nil
}

// Get returns the session with the given id.
func (r *PostgresRepository) Get(ctx context.Context, id string) (*Session, error) {
	var (
		s           Session
		assistantID uuid.UUID
	)

	if err := r.pool.QueryRow(ctx, getSessionQuery, id).Scan(
		&s.ID,
		&assistantID,
		&s.CreatedAt,
		&s.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf(getErrMsg, ErrSessionNotFound)
		}

		return nil, fmt.Errorf(getErrMsg, err)
	}
	s.UserID = assistantID.String()

	return &s, nil
}

func (r *PostgresRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, deleteSessionQuery, id)
	if err != nil {
		return fmt.Errorf(deleteErrMsg, err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf(deleteErrMsg, ErrSessionNotFound)
	}

	return nil
}

// DeleteByAssistant removes every session the assistant holds and reports how
// many. Unlike Delete, zero rows is a normal result rather than ErrSessionNotFound.
func (r *PostgresRepository) DeleteByAssistant(ctx context.Context, userID string) (int64, error) {
	assistantID, err := domain.ParseID(userID)
	if err != nil {
		return 0, fmt.Errorf(deleteByAssistantErrMsg, fmt.Errorf("%w: %w", ErrInvalidAssistantID, err))
	}

	tag, err := r.pool.Exec(ctx, deleteAssistantSessionsQuery, assistantID)
	if err != nil {
		return 0, fmt.Errorf(deleteByAssistantErrMsg, err)
	}

	return tag.RowsAffected(), nil
}

// DeleteExpired removes every session that expired before the given instant.
// The cutoff is a parameter, not Postgres's now(), so the sweep uses the same
// clock that wrote expires_at.
func (r *PostgresRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, deleteExpiredSessionsQuery, before)
	if err != nil {
		return 0, fmt.Errorf(deleteExpiredErrMsg, err)
	}

	return tag.RowsAffected(), nil
}

func mapPgxError(errMsg string, err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return fmt.Errorf(errMsg, err)
	}

	if pgErr.ConstraintName == constraintFkSessionAssistant {
		return fmt.Errorf(errMsg, ErrUnknownAssistant)
	}

	return fmt.Errorf(errMsg, err)
}
