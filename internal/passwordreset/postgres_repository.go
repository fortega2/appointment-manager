package passwordreset

import (
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
	constraintFkPasswordResetTokenAssistant = "fk_password_reset_token_assistant"

	createErrMsg            = "create password reset token: %w"
	getErrMsg               = "get password reset token: %w"
	consumeErrMsg           = "consume password reset token: %w"
	deleteExpiredErrMsg     = "delete expired password reset tokens: %w"
	deleteByAssistantErrMsg = "delete password reset tokens by assistant: %w"

	// Upsert, not delete-then-insert: two concurrent issues for the same account
	// would both find nothing to delete and leave two live links behind.
	//nolint:gosec // G101 false positive: a SQL statement, not a credential.
	createPasswordResetTokenQuery = `
		INSERT INTO public.password_reset_token (
			id,
			assistant_id,
			created_at,
			expires_at
		) VALUES ($1, $2, $3, $4)
		ON CONFLICT (assistant_id) DO UPDATE SET
			id = EXCLUDED.id,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
	`

	//nolint:gosec // G101 false positive: a SQL statement, not a credential.
	getPasswordResetTokenQuery = `
		SELECT
			id,
			assistant_id,
			created_at,
			expires_at
		FROM
			public.password_reset_token
		WHERE
			id = $1
	`

	// Redeeming is a delete rather than a flag update, so the row is gone the
	// moment it is handed out. Two requests racing on the same link cannot both
	// come back with an assistant id.
	//nolint:gosec // G101 false positive: a SQL statement, not a credential.
	consumePasswordResetTokenQuery = `
		DELETE FROM
			public.password_reset_token
		WHERE
			id = $1
		RETURNING
			id,
			assistant_id,
			created_at,
			expires_at
	`

	//nolint:gosec // G101 false positive: a SQL statement, not a credential.
	deleteExpiredPasswordResetTokensQuery = `
		DELETE FROM
			public.password_reset_token
		WHERE
			expires_at < $1
	`

	//nolint:gosec // G101 false positive: a SQL statement, not a credential.
	deletePasswordResetTokensByAssistantQuery = `
		DELETE FROM
			public.password_reset_token
		WHERE
			assistant_id = $1
	`
)

// PostgresRepository persists reset tokens. Every id it receives is already a
// token digest; it never sees the token itself.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &PostgresRepository{pool: pool}, nil
}

// Create stores the token and drops whatever the assistant already had.
func (r *PostgresRepository) Create(ctx context.Context, t Token) error {
	if _, err := r.pool.Exec(ctx, createPasswordResetTokenQuery,
		t.ID,
		t.AssistantID,
		t.CreatedAt,
		t.ExpiresAt,
	); err != nil {
		return mapPgxError(createErrMsg, err)
	}

	return nil
}

// Get returns the token with the given id without spending it.
func (r *PostgresRepository) Get(ctx context.Context, id string) (*Token, error) {
	var t Token

	if err := r.pool.QueryRow(ctx, getPasswordResetTokenQuery, id).Scan(
		&t.ID,
		&t.AssistantID,
		&t.CreatedAt,
		&t.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf(getErrMsg, ErrTokenNotFound)
		}

		return nil, fmt.Errorf(getErrMsg, err)
	}

	return &t, nil
}

// Consume deletes the token with the given id and returns it.
func (r *PostgresRepository) Consume(ctx context.Context, id string) (*Token, error) {
	var t Token

	if err := r.pool.QueryRow(ctx, consumePasswordResetTokenQuery, id).Scan(
		&t.ID,
		&t.AssistantID,
		&t.CreatedAt,
		&t.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf(consumeErrMsg, ErrTokenNotFound)
		}

		return nil, fmt.Errorf(consumeErrMsg, err)
	}

	return &t, nil
}

// DeleteExpired removes every token that expired before the given instant. The
// cutoff is a parameter, not Postgres's now(), so the sweep uses the same clock
// that wrote expires_at.
func (r *PostgresRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, deleteExpiredPasswordResetTokensQuery, before)
	if err != nil {
		return 0, fmt.Errorf(deleteExpiredErrMsg, err)
	}

	return tag.RowsAffected(), nil
}

// DeleteByAssistant revokes every outstanding link the assistant holds.
func (r *PostgresRepository) DeleteByAssistant(ctx context.Context, assistantID uuid.UUID) (int64, error) {
	tag, err := r.pool.Exec(ctx, deletePasswordResetTokensByAssistantQuery, assistantID)
	if err != nil {
		return 0, fmt.Errorf(deleteByAssistantErrMsg, err)
	}

	return tag.RowsAffected(), nil
}

func mapPgxError(errMsg string, err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return fmt.Errorf(errMsg, err)
	}

	if pgErr.ConstraintName == constraintFkPasswordResetTokenAssistant {
		return fmt.Errorf(errMsg, ErrUnknownAssistant)
	}

	return fmt.Errorf(errMsg, err)
}
