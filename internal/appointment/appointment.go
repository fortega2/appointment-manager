package appointment

import (
	"fmt"

	"github.com/google/uuid"
)

type Status int16

const (
	StatusConfirmed Status = iota + 1
	StatusCancelled
	StatusAbsent
	StatusAttended
)

func parseStatus(value int) (Status, error) {
	switch value {
	case int(StatusConfirmed):
		return StatusConfirmed, nil
	case int(StatusCancelled):
		return StatusCancelled, nil
	case int(StatusAbsent):
		return StatusAbsent, nil
	case int(StatusAttended):
		return StatusAttended, nil
	default:
		return 0, fmt.Errorf("%w: %d", ErrInvalidStatus, value)
	}
}

type Appointment struct {
	ID             uuid.UUID `json:"id"`
	SlotID         uuid.UUID `json:"slot_id"`
	PatientID      uuid.UUID `json:"patient_id"`
	ProfessionalID uuid.UUID `json:"professional_id"`
	AssistantID    uuid.UUID `json:"assistant_id"`
	Status         Status    `json:"status"`
	Notes          *string   `json:"notes,omitempty"`
}

func NewAppointment(slotID, patientID, professionalID, assistantID uuid.UUID, notes *string) *Appointment {
	return &Appointment{
		ID:             uuid.New(),
		SlotID:         slotID,
		PatientID:      patientID,
		ProfessionalID: professionalID,
		AssistantID:    assistantID,
		Status:         StatusConfirmed,
		Notes:          notes,
	}
}
