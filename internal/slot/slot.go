package slot

import (
	"fmt"
	"time"

	"github.com/google/uuid"
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
		ID:             uuid.New(),
		ProfessionalID: professionalID,
		Date:           date,
		StartTime:      startTime,
		EndTime:        endTime,
		MaxCapacity:    maxCapacity,
	}, nil
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

	const fullDayDuration time.Duration = 24 * time.Hour
	if !date.Equal(startTime.Truncate(fullDayDuration)) {
		return ErrDateTimeInconsistency
	}

	return nil
}
