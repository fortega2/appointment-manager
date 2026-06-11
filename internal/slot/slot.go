package slot

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"appointment-manager/internal/domain"
)

type Slot struct {
	Date           time.Time
	StartTime      time.Time
	EndTime        time.Time
	ID             uuid.UUID
	ProfessionalID uuid.UUID
	MaxCapacity    int16
	Blocked        bool
}

func NewSlot(professionalID uuid.UUID, date time.Time, startTime time.Time, endTime time.Time, maxCapacity int16) (*Slot, error) {
	if err := validateSlot(professionalID, date, startTime, endTime, maxCapacity); err != nil {
		return nil, fmt.Errorf("validate slot: %w", err)
	}

	return &Slot{
		ID:             domain.NewID(),
		ProfessionalID: professionalID,
		Date:           date,
		StartTime:      startTime,
		EndTime:        endTime,
		MaxCapacity:    maxCapacity,
	}, nil
}

func (s *Slot) Update(professionalID uuid.UUID, date time.Time, startTime time.Time, endTime time.Time, maxCapacity int16, blocked bool) error {
	if err := validateSlot(professionalID, date, startTime, endTime, maxCapacity); err != nil {
		return fmt.Errorf("validate slot: %w", err)
	}
	s.ProfessionalID = professionalID
	s.Date = date
	s.StartTime = startTime
	s.EndTime = endTime
	s.MaxCapacity = maxCapacity
	s.Blocked = blocked
	return nil
}

func validateSlot(professionalID uuid.UUID, date time.Time, startTime time.Time, endTime time.Time, maxCapacity int16) error {
	if professionalID == uuid.Nil {
		return ErrInvalidProfessionalID
	}

	if endTime.Before(startTime) || endTime.Equal(startTime) {
		return ErrInvalidTimeRange
	}

	if maxCapacity <= 0 {
		return ErrInvalidMaxCapacity
	}

	if date.IsZero() {
		return ErrInvalidDate
	}

	y, m, d := startTime.Date()
	expectedDate := time.Date(y, m, d, 0, 0, 0, 0, startTime.Location())
	if !date.Equal(expectedDate) {
		return ErrDateTimeInconsistency
	}

	return nil
}
