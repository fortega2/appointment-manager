package slot

import (
	"fmt"
	"time"
	"uuid"

	"appointment-manager/internal/domain"
	"appointment-manager/internal/outbox"
)

// Outbox vocabulary for slot events. Declared here because this package owns
// the aggregate, even though appointment is what writes them.
const (
	OutboxAggregate            outbox.AggregateType = "slot"
	EventAppointmentsCancelled outbox.EventType     = "slot.appointments_cancelled"
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

func validateSlot(professionalID uuid.UUID, date time.Time, startTime time.Time, endTime time.Time, maxCapacity int16) error {
	if professionalID == uuid.Nil() {
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
