package appointment

import (
	"github.com/google/uuid"
)

type Status uint

const (
	StatusConfirmed Status = iota + 1
	StatusCancelled
	StatusAbsent
	StatusAttended
)

type Appointment struct {
	ID             uuid.UUID `json:"id"`
	SlotID         uuid.UUID `json:"slot_id"`
	PatientID      uuid.UUID `json:"patient_id"`
	ProfessionalID uuid.UUID `json:"professional_id"`
	AssistantID    uuid.UUID `json:"assistant_id"`
	Status         Status    `json:"status"`
}

func NewAppointment(slotID, patientID, professionalID, assistantID uuid.UUID) *Appointment {
	return &Appointment{
		ID:             uuid.New(),
		SlotID:         slotID,
		PatientID:      patientID,
		ProfessionalID: professionalID,
		AssistantID:    assistantID,
		Status:         StatusConfirmed,
	}
}
