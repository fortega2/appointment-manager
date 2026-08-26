package appointment

import (
	"fmt"
	"uuid"

	"appointment-manager/internal/domain"
)

type Status int16

// The values are pinned by the appointment_status lookup table, which
// appointment.status references through fk_appointment_status. A constant added
// here without its lookup row fails closed: the first write rejects on the
// foreign key rather than storing an unknown status.
const (
	StatusConfirmed Status = iota + 1
	StatusCancelled
	StatusAbsent
	StatusAttended
	// StatusCancelledByClinic marks an appointment the clinic called off by
	// cancelling its whole slot, as opposed to StatusCancelled, which the
	// patient asked for. Keeping them apart is what lets notifications reach
	// only the patients the clinic owes a message.
	StatusCancelledByClinic
)

// IsCancelled reports whether the appointment was called off by either party.
// Cancellation spans two statuses, so comparing against StatusCancelled alone
// silently misses clinic-initiated cancellations.
func (s Status) IsCancelled() bool {
	return s == StatusCancelled || s == StatusCancelledByClinic
}

// LabelKey is the catalog key for the status name shown on screen.
func (s Status) LabelKey() string {
	switch s {
	case StatusConfirmed:
		return statusKeyConfirmed
	case StatusCancelled:
		return statusKeyCancelled
	case StatusAbsent:
		return statusKeyAbsent
	case StatusAttended:
		return statusKeyAttended
	case StatusCancelledByClinic:
		return statusKeyCancelledByClinic
	default:
		return statusKeyUnknown
	}
}

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
	case int(StatusCancelledByClinic):
		return StatusCancelledByClinic, nil
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
		ID:             domain.NewID(),
		SlotID:         slotID,
		PatientID:      patientID,
		ProfessionalID: professionalID,
		AssistantID:    assistantID,
		Status:         StatusConfirmed,
		Notes:          notes,
	}
}
