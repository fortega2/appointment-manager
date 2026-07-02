package prescription

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
	indexActivePrescriptionPerPatient = "idx_prescription_active_per_patient"
	constraintFkPrescriptionPatient   = "fk_prescription_patient"

	insertPrescriptionQuery = `
		INSERT INTO prescription (
			id,
			patient_id,
			file_path,
			total_sessions,
			status
		) VALUES ($1, $2, $3, $4, $5)
	`

	getActivePrescriptionByPatientQuery = `
		SELECT
			id,
			patient_id,
			file_path,
			total_sessions,
			status
		FROM
			prescription
		WHERE
			patient_id = $1 AND status = 1
	`

	getPrescriptionByIDQuery = `
		SELECT
			id,
			patient_id,
			file_path,
			total_sessions,
			status
		FROM
			prescription
		WHERE
			id = $1
	`

	updatePrescriptionStatusQuery = `
		UPDATE
			prescription
		SET
			status = $1
		WHERE
			id = $2
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

func (r *Repository) Create(ctx context.Context, p *Prescription) error {
	if p == nil {
		return ErrNilPrescription
	}

	if _, err := r.pool.Exec(
		ctx,
		insertPrescriptionQuery,
		p.ID,
		p.PatientID,
		p.FilePath,
		p.TotalSessions,
		p.Status,
	); err != nil {
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		if !ok {
			return fmt.Errorf("failed to create prescription: %w", err)
		}

		if pgErr.ConstraintName == indexActivePrescriptionPerPatient {
			return ErrActivePrescriptionExists
		}

		if pgErr.ConstraintName == constraintFkPrescriptionPatient {
			return ErrInvalidPatient
		}

		return fmt.Errorf("failed to create prescription: %w", err)
	}

	return nil
}

func (r *Repository) GetActiveByPatient(ctx context.Context, patientID uuid.UUID) (*Prescription, error) {
	var p Prescription
	if err := r.pool.QueryRow(ctx, getActivePrescriptionByPatientQuery, patientID).Scan(
		&p.ID,
		&p.PatientID,
		&p.FilePath,
		&p.TotalSessions,
		&p.Status,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("get active prescription for patient %s: %w", patientID, ErrNoActivePrescription)
		}
		return nil, fmt.Errorf("get active prescription by patient: %w", err)
	}

	return &p, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Prescription, error) {
	var p Prescription
	if err := r.pool.QueryRow(ctx, getPrescriptionByIDQuery, id).Scan(
		&p.ID,
		&p.PatientID,
		&p.FilePath,
		&p.TotalSessions,
		&p.Status,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("get prescription by id %s: %w", id, ErrPrescriptionNotFound)
		}
		return nil, fmt.Errorf("get prescription by id: %w", err)
	}

	return &p, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) error {
	res, err := r.pool.Exec(ctx, updatePrescriptionStatusQuery, status, id)
	if err != nil {
		return fmt.Errorf("failed to update prescription status: %w", err)
	}

	if res.RowsAffected() == 0 {
		return ErrPrescriptionNotFound
	}

	return nil
}
