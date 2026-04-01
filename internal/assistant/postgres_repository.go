package assistant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
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
	createQuery = `
		INSERT INTO assistant (
			id,
			first_name,
			last_name,
			email,
			password_hash
		) VALUES ($1, $2, $3, $4, $5)
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
			return uuid.Nil, ErrEmailAlreadyExists
		}
		return uuid.Nil, fmt.Errorf("Create: %w", err)
	}

	return assistant.ID, nil
}

func isUniqueEmailViolation(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}

	return pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintAssistantEmailUnique
}
