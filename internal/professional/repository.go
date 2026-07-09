package professional

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
	constraintCheckProfessionalSpecialty string = "chk_professional_specialty"

	insertProfessionalQuery string = `
		INSERT INTO professional (
			id,
			first_name,
			last_name,
			phone
		) VALUES ($1, $2, $3, $4)
	`
	listProfessionalsQuery string = `
		SELECT
			id,
			first_name,
			last_name,
			phone,
			INITCAP(specialty) AS specialty,
			active
		FROM
			professional
		WHERE
			active = true
		ORDER BY
			created_at DESC
	`

	listAllProfessionalsQuery string = `
		SELECT
			id,
			first_name,
			last_name,
			phone,
			INITCAP(specialty) AS specialty,
			active
		FROM
			professional
		ORDER BY
			created_at DESC
	`

	getProfessionalByIDQuery string = `
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
			id = $1
	`

	updateProfessionalQuery string = `
		UPDATE
			professional
		SET
			first_name = $1,
			last_name = $2,
			phone = $3,
			active = $4,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			id = $5
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

// List returns only active professionals. Use this for booking flows and any
// other consumer that must not offer a deactivated professional.
func (r *Repository) List(ctx context.Context) ([]Professional, error) {
	return r.queryProfessionals(ctx, listProfessionalsQuery)
}

// ListAll returns every professional regardless of active status. Use this
// for the management dashboard, where staff need to see and reactivate
// inactive professionals.
func (r *Repository) ListAll(ctx context.Context) ([]Professional, error) {
	return r.queryProfessionals(ctx, listAllProfessionalsQuery)
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Professional, error) {
	var p Professional
	if err := r.pool.QueryRow(ctx, getProfessionalByIDQuery, id).Scan(
		&p.ID,
		&p.FirstName,
		&p.LastName,
		&p.Phone,
		&p.Specialty,
		&p.Active,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("get professional by id %s: %w", id, ErrProfessionalNotFound)
		}
		return nil, fmt.Errorf("get professional by id: %w", err)
	}

	return &p, nil
}

func (r *Repository) Update(ctx context.Context, p *Professional) error {
	if p == nil {
		return ErrNilProfessional
	}

	cmdTag, err := r.pool.Exec(
		ctx,
		updateProfessionalQuery,
		p.FirstName,
		p.LastName,
		p.Phone,
		p.Active,
		p.ID,
	)
	if err != nil {
		return fmt.Errorf("update professional: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("update professional: %w", ErrProfessionalNotFound)
	}

	return nil
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

func (r *Repository) queryProfessionals(ctx context.Context, query string) ([]Professional, error) {
	rows, err := r.pool.Query(ctx, query)
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
