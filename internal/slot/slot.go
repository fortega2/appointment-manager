package slot

import (
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
	if professionalID == uuid.Nil {
		return nil, ErrInvalidProfessionalID
	}
	if endTime.Before(startTime) || endTime.Equal(startTime) {
		return nil, ErrInvalidTimeRange
	}
	if maxCapacity <= 0 {
		return nil, ErrInvalidMaxCapacity
	}
	if date.IsZero() {
		return nil, ErrInvalidDate
	}
	const fullDayDuration time.Duration = 24 * time.Hour
	if !date.Equal(startTime.Truncate(fullDayDuration)) {
		return nil, ErrDateTimeInconsistency
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
