package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	constraintAssistantEmailUnique = "assistant_email_key"
	pgErrUniqueViolation           = "23505"
	listErrMsg                     = "List: %w"

	listQuery = `
		SELECT
			id,
			first_name,
			last_name,
			email,
			password_hash
		FROM
			assistant
	`
	getQuery = `
		SELECT
			id,
			first_name,
			last_name,
			email,
			password_hash
		FROM
			assistant
		WHERE
			id = $1
	`
	getByEmailQuery = `
		SELECT
			id,
			first_name,
			last_name,
			email,
			password_hash
		FROM
			assistant
		WHERE
			email = $1
	`
	createQuery = `
		INSERT INTO assistant (
			id,
			first_name,
			last_name,
			email,
			password_hash
		) VALUES ($1, $2, $3, $4, $5)
	`
	// updated_at is stamped by the database, the clock created_at already
	// defaults to, so skew can never order the pair backwards.
	//nolint:gosec // G101 false positive: a SQL statement, not a credential.
	updatePasswordHashQuery = `
		UPDATE
			assistant
		SET
			password_hash = $2,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			id = $1
	`
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &PostgresRepository{
		pool: pool,
	}, nil
}

func (r *PostgresRepository) List(ctx context.Context) ([]Assistant, error) {
	rows, err := r.pool.Query(ctx, listQuery)
	if err != nil {
		return nil, fmt.Errorf(listErrMsg, err)
	}
	defer rows.Close()

	assistants := make([]Assistant, 0)
	for rows.Next() {
		var assistant Assistant
		if err := rows.Scan(
			&assistant.ID,
			&assistant.FirstName,
			&assistant.LastName,
			&assistant.Email,
			&assistant.PasswordHash,
		); err != nil {
			return nil, fmt.Errorf(listErrMsg, err)
		}
		assistants = append(assistants, assistant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(listErrMsg, err)
	}

	return assistants, nil
}

func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (*Assistant, error) {
	row := r.pool.QueryRow(ctx, getQuery, id)

	var assistant Assistant
	if err := row.Scan(
		&assistant.ID,
		&assistant.FirstName,
		&assistant.LastName,
		&assistant.Email,
		&assistant.PasswordHash,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %w", ErrAssistantNotFound, err)
		}
		return nil, fmt.Errorf("Get: %w", err)
	}

	return &assistant, nil
}

func (r *PostgresRepository) GetByEmail(ctx context.Context, email string) (*Assistant, error) {
	row := r.pool.QueryRow(ctx, getByEmailQuery, email)

	var assistant Assistant
	if err := row.Scan(
		&assistant.ID,
		&assistant.FirstName,
		&assistant.LastName,
		&assistant.Email,
		&assistant.PasswordHash,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %w", ErrAssistantNotFound, err)
		}
		return nil, fmt.Errorf("GetByEmail: %w", err)
	}

	return &assistant, nil
}

func (r *PostgresRepository) Create(ctx context.Context, assistant Assistant) (uuid.UUID, error) {
	_, err := r.pool.Exec(ctx, createQuery,
		assistant.ID,
		assistant.FirstName,
		assistant.LastName,
		assistant.Email,
		assistant.PasswordHash,
	)
	if err != nil {
		if isUniqueEmailViolation(err) {
			return uuid.Nil(), ErrEmailAlreadyExists
		}
		return uuid.Nil(), fmt.Errorf("Create: %w", err)
	}

	return assistant.ID, nil
}

// UpdatePasswordHash replaces the stored hash. A blank hash is refused because
// no password could ever verify against it, and an id matching no row is
// ErrAssistantNotFound rather than a silent success.
func (r *PostgresRepository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	if strings.TrimSpace(passwordHash) == "" {
		return ErrEmptyPasswordHash
	}

	tag, err := r.pool.Exec(ctx, updatePasswordHashQuery, id, passwordHash)
	if err != nil {
		return fmt.Errorf("UpdatePasswordHash: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("UpdatePasswordHash: %w", ErrAssistantNotFound)
	}

	return nil
}

func isUniqueEmailViolation(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}

	return pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintAssistantEmailUnique
}
