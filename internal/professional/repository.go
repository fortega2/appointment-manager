package professional

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	constraintCheckProfessionalSpecialty = "chk_professional_specialty"

	insertProfessionalQuery = `
		INSERT INTO professional (
			id,
			first_name,
			last_name,
			phone
		) VALUES ($1, $2, $3, $4)
	`
	listProfessionalsQuery = `
		SELECT
			id,
			first_name,
			last_name,
			phone,
			specialty,
			active
		FROM
			professional
		WHERE
			active = true
	`
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &Repository{pool: pool}, nil
}

func (r *Repository) Create(ctx context.Context, p *Professional) error {
	if p == nil {
		return ErrNilProfessional
	}

	if _, err := r.pool.Exec(
		ctx,
		insertProfessionalQuery,
		p.ID,
		p.FirstName,
		p.LastName,
		p.Phone,
	); err != nil {
		return r.mapCreateError(err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context) ([]Professional, error) {
	rows, err := r.pool.Query(ctx, listProfessionalsQuery)
	if err != nil {
		return nil, fmt.Errorf("query professionals: %w", err)
	}
	defer rows.Close()

	professionals := make([]Professional, 0)
	for rows.Next() {
		var item Professional
		if err := rows.Scan(
			&item.ID,
			&item.FirstName,
			&item.LastName,
			&item.Phone,
			&item.Specialty,
			&item.Active,
		); err != nil {
			return nil, fmt.Errorf("scan professional: %w", err)
		}
		professionals = append(professionals, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate professionals: %w", err)
	}

	return professionals, nil
}

func (r *Repository) mapCreateError(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return fmt.Errorf("create professional: %w", err)
	}

	switch pgErr.ConstraintName {
	case constraintCheckProfessionalSpecialty:
		return ErrInvalidProfessionalSpecialty
	default:
		return fmt.Errorf("create professional: %w", err)
	}
}
