package patient

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	constraintFkPatientHealthInsurance = "fk_patient_health_insurance"

	insertPatientQuery = `
		INSERT INTO patient (
			id,
			first_name,
			last_name,
			phone,
			email,
			health_insurance,
			insurance_number,
			clinical_notes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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

func (r *Repository) Create(ctx context.Context, p *Patient) error {
	if p == nil {
		return ErrNilPatient
	}

	if _, err := r.pool.Exec(
		ctx,
		insertPatientQuery,
		p.ID,
		p.FirstName,
		p.LastName,
		p.Phone,
		p.Email,
		p.HealthInsurance,
		p.InsuranceNumber,
		p.ClinicalNotes,
	); err != nil {
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		if !ok {
			return fmt.Errorf("failed to create patient: %w", err)
		}

		if pgErr.ConstraintName == constraintFkPatientHealthInsurance {
			return ErrInvalidHealthInsurance
		}

		return fmt.Errorf("failed to create patient: %w", err)
	}

	return nil
}
