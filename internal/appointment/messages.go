package appointment

// Catalog keys for the copy the UI handler shows the user. The Go errors keep
// their English text for the logs and for the JSON API, which is machine-facing;
// these are what reaches the screen.
const (
	msgKeyCreated   = "appointment.message.created"
	msgKeyAttended  = "appointment.message.attended"
	msgKeyCancelled = "appointment.message.cancelled"

	errKeyLoadSlots            = "appointment.error.load_slots"
	errKeyLoadPatients         = "appointment.error.load_patients"
	errKeyInvalidID            = "appointment.error.invalid_id"
	errKeyInvalidForm          = "appointment.error.invalid_form"
	errKeySession              = "appointment.error.session"
	errKeyNotFound             = "appointment.error.not_found"
	errKeyCannotAttendNow      = "appointment.error.cannot_attend_now"
	errKeyCannotAttendStatus   = "appointment.error.cannot_attend_status"
	errKeyCannotCancelStatus   = "appointment.error.cannot_cancel_status"
	errKeyStatusChanged        = "appointment.error.status_changed"
	errKeyProcess              = "appointment.error.process"
	errKeySlotRequired         = "appointment.error.slot_required"
	errKeyInvalidSlot          = "appointment.error.invalid_slot"
	errKeyPatientRequired      = "appointment.error.patient_required"
	errKeyInvalidPatient       = "appointment.error.invalid_patient"
	errKeyProfessionalRequired = "appointment.error.professional_required"
	errKeyInvalidProfessional  = "appointment.error.invalid_professional"
	errKeyAlreadyActive        = "appointment.error.already_active"
	errKeySlotBlocked          = "appointment.error.slot_blocked"
	errKeySlotNoAvailability   = "appointment.error.slot_no_availability"
	errKeyNoPrescription       = "appointment.error.no_prescription"
	errKeyNoRemainingSessions  = "appointment.error.no_remaining_sessions"
	errKeyReferenceNotFound    = "appointment.error.reference_not_found"
	errKeyCreate               = "appointment.error.create"
)
