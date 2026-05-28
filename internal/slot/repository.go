package slot

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createSlotErrorMsg               = "create slot: %w"
	constraintFkSlotProfessional     = "fk_slot_professional"
	constraintChkSlotTimes           = "chk_slot_times"
	constraintChkSlotCapacity        = "chk_slot_capacity"
	constraintChkSlotDateConsistency = "chk_slot_date_consistency"
	constraintChkNoOverlappingSlots  = "chk_no_overlapping_slots"
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

func (r *Repository) Create(ctx context.Context, s *Slot) error {
	if s == nil {
		return ErrNilSlot
	}
	const query string = `
		INSERT INTO public.slot (
			id,
			professional_id,
			date,
			start_time,
			end_time,
			max_capacity
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	if _, err := r.pool.Exec(
		ctx,
		query,
		s.ID,
		s.ProfessionalID,
		s.Date,
		s.StartTime,
		s.EndTime,
		s.MaxCapacity,
	); err != nil {
		return r.mapCreateError(err)
	}

	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Slot, error) {
	const query string = `
		SELECT
			id,
			professional_id,
			date,
			start_time,
			end_time,
			max_capacity,
			blocked
		FROM
			public.slot
		WHERE
			id = $1
	`
	var s Slot
	if err := r.pool.QueryRow(ctx, query, id).Scan(
		&s.ID,
		&s.ProfessionalID,
		&s.Date,
		&s.StartTime,
		&s.EndTime,
		&s.MaxCapacity,
		&s.Blocked,
	); err != nil {
		return nil, fmt.Errorf("get slot by id: %w", err)
	}

	return &s, nil
}

func (r *Repository) Update(ctx context.Context, s *Slot) error {
	if s == nil {
		return ErrNilSlot
	}
	const query string = `
		UPDATE public.slot
		SET
			professional_id = $2,
			date = $3,
			start_time = $4,
			end_time = $5,
			max_capacity = $6,
			blocked = $7,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			id = $1
	`

	if _, err := r.pool.Exec(
		ctx,
		query,
		s.ID,
		s.ProfessionalID,
		s.Date,
		s.StartTime,
		s.EndTime,
		s.MaxCapacity,
		s.Blocked,
	); err != nil {
		return r.mapCreateError(err)
	}

	return nil
}

func (r *Repository) mapCreateError(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return fmt.Errorf(createSlotErrorMsg, err)
	}

	switch pgErr.ConstraintName {
	case constraintFkSlotProfessional:
		return fmt.Errorf(createSlotErrorMsg, ErrInvalidProfessionalID)
	case constraintChkSlotTimes:
		return fmt.Errorf(createSlotErrorMsg, ErrInvalidTimeRange)
	case constraintChkSlotCapacity:
		return fmt.Errorf(createSlotErrorMsg, ErrInvalidMaxCapacity)
	case constraintChkSlotDateConsistency:
		return fmt.Errorf(createSlotErrorMsg, ErrDateTimeInconsistency)
	case constraintChkNoOverlappingSlots:
		return fmt.Errorf(createSlotErrorMsg, ErrSlotOverlaps)
	default:
		return fmt.Errorf(createSlotErrorMsg, err)
	}
}
