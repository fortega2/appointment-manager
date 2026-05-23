package slot

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createSlotErrorMsg               = "create slot: %w"
	constraintFkSlotProfessional     = "fk_slot_professional"
	constraintChkSlotTimes           = "chk_slot_times"
	constraintChkSlotCapacity        = "chk_slot_capacity"
	constraintChkSlotDateConsistency = "chk_slot_date_consistency"
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
	default:
		return fmt.Errorf(createSlotErrorMsg, err)
	}
}
