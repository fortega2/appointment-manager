package prescription

// Catalog keys for the copy the UI handler shows the user. The Go errors keep
// their English text for the logs; these are what reaches the screen.
const (
	msgKeyCreated   = "prescription.message.created"
	msgKeyCancelled = "prescription.message.cancelled"

	errKeyInvalidForm     = "prescription.error.invalid_form"
	errKeyInvalidID       = "prescription.error.invalid_id"
	errKeyUnsupportedFile = "prescription.error.unsupported_file"
	errKeyInvalidPatient  = "prescription.error.invalid_patient"
	errKeyAlreadyActive   = "prescription.error.already_active"
	errKeyCreate          = "prescription.error.create"
	errKeyNotFound        = "prescription.error.not_found"
	errKeyCancel          = "prescription.error.cancel"
)
