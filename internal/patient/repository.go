package patient

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

	listPatientsQuery = `
		SELECT
			p.id,
			p.first_name,
			p.last_name,
			p.phone,
			p.email,
			p.health_insurance,
			INITCAP(h.name) AS health_insurance_name,
			p.insurance_number,
			p.clinical_notes
		FROM
			patient p
		INNER JOIN
			health_insurance h ON p.health_insurance = h.id
		ORDER BY
			p.created_at DESC
	`

	getPatientByIDQuery = `
		SELECT
			p.id,
			p.first_name,
			p.last_name,
			p.phone,
			p.email,
			p.health_insurance,
			INITCAP(h.name) AS health_insurance_name,
			p.insurance_number,
			p.clinical_notes
		FROM
			patient p
		INNER JOIN
			health_insurance h ON p.health_insurance = h.id
		WHERE
			p.id = $1
	`

	updatePatientQuery = `
		UPDATE
			patient
		SET
			first_name = $1,
			last_name = $2,
			phone = $3,
			email = $4,
			health_insurance = $5,
			insurance_number = $6,
			clinical_notes = $7,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			id = $8
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

func (r *Repository) List(ctx context.Context) ([]View, error) {
	rows, err := r.pool.Query(ctx, listPatientsQuery)
	if err != nil {
		return nil, fmt.Errorf("query patients: %w", err)
	}
	defer rows.Close()

	var patients []View
	for rows.Next() {
		var p View
		if err := rows.Scan(
			&p.ID,
			&p.FirstName,
			&p.LastName,
			&p.Phone,
			&p.Email,
			&p.HealthInsurance,
			&p.HealthInsuranceName,
			&p.InsuranceNumber,
			&p.ClinicalNotes,
		); err != nil {
			return nil, fmt.Errorf("scan patient: %w", err)
		}
		patients = append(patients, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate patient rows: %w", err)
	}

	return patients, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*View, error) {
	var p View
	if err := r.pool.QueryRow(ctx, getPatientByIDQuery, id).Scan(
		&p.ID,
		&p.FirstName,
		&p.LastName,
		&p.Phone,
		&p.Email,
		&p.HealthInsurance,
		&p.HealthInsuranceName,
		&p.InsuranceNumber,
		&p.ClinicalNotes,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("get patient by id %s: %w", id, ErrPatientNotFound)
		}
		return nil, fmt.Errorf("get patient by id: %w", err)
	}

	return &p, nil
}

func (r *Repository) Update(ctx context.Context, p *Patient) error {
	if p == nil {
		return ErrNilPatient
	}

	res, err := r.pool.Exec(
		ctx,
		updatePatientQuery,
		p.FirstName,
		p.LastName,
		p.Phone,
		p.Email,
		p.HealthInsurance,
		p.InsuranceNumber,
		p.ClinicalNotes,
		p.ID,
	)
	if err != nil {
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		if !ok {
			return fmt.Errorf("failed to update patient: %w", err)
		}

		if pgErr.ConstraintName == constraintFkPatientHealthInsurance {
			return ErrInvalidHealthInsurance
		}

		return fmt.Errorf("failed to update patient: %w", err)
	}

	if res.RowsAffected() == 0 {
		return ErrPatientNotFound
	}

	return nil
}
