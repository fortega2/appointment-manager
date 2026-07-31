package slot

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
	createSlotErrorMsg               = "create slot: %w"
	cancelSlotErrorMsg               = "cancel slot: %w"
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
		return r.mapPgxError(createSlotErrorMsg, err)
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

func (r *Repository) Cancel(ctx context.Context, id uuid.UUID) error {
	const query string = `
		UPDATE public.slot
		SET
			blocked = TRUE,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			id = $1 AND blocked = FALSE
	`

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return r.mapPgxError(cancelSlotErrorMsg, err)
	}

	if tag.RowsAffected() == 0 {
		if err := r.slotExists(ctx, id); err != nil {
			return fmt.Errorf(cancelSlotErrorMsg, err)
		}

		return fmt.Errorf(cancelSlotErrorMsg, ErrSlotAlreadyCancelled)
	}

	return nil
}

// slotExists reports whether a slot row with the given id exists, returning
// ErrSlotNotFound if not. It is used to disambiguate a zero-rows-affected
// conditional update between "no such slot" and "the condition didn't match
// because the slot's state already changed" (e.g. a concurrent cancel).
func (r *Repository) slotExists(ctx context.Context, id uuid.UUID) error {
	const query string = `SELECT 1 FROM public.slot WHERE id = $1`

	var exists int
	if err := r.pool.QueryRow(ctx, query, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSlotNotFound
		}

		return fmt.Errorf("check slot existence: %w", err)
	}

	return nil
}

func (r *Repository) mapPgxError(errMsg string, err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return fmt.Errorf(errMsg, err)
	}

	switch pgErr.ConstraintName {
	case constraintFkSlotProfessional:
		return fmt.Errorf(errMsg, ErrInvalidProfessionalID)
	case constraintChkSlotTimes:
		return fmt.Errorf(errMsg, ErrInvalidTimeRange)
	case constraintChkSlotCapacity:
		return fmt.Errorf(errMsg, ErrInvalidMaxCapacity)
	case constraintChkSlotDateConsistency:
		return fmt.Errorf(errMsg, ErrDateTimeInconsistency)
	case constraintChkNoOverlappingSlots:
		return fmt.Errorf(errMsg, ErrSlotOverlaps)
	default:
		return fmt.Errorf(errMsg, err)
	}
}
