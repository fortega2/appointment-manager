package slot

import "errors"

// Catalog keys for the copy this package shows the user. The Go errors keep
// their English text for the logs; these are what reaches the screen.
const (
	msgKeyCreated   = "slot.message.created"
	msgKeyCancelled = "slot.message.cancelled"

	errKeyLoadSlots             = "slot.error.load_slots"
	errKeyLoadProfessionals     = "slot.error.load_professionals"
	errKeyParseForm             = "slot.error.parse_form"
	errKeyInvalidID             = "slot.error.invalid_id"
	errKeyNotFound              = "slot.error.not_found"
	errKeyAlreadyCancelled      = "slot.error.already_cancelled"
	errKeyCreate                = "slot.error.create"
	errKeyCancel                = "slot.error.cancel"
	errKeyCancelAppointments    = "slot.error.cancel_appointments"
	errKeyOverlaps              = "slot.error.overlaps"
	errKeyUnexpected            = "common.error.unexpected"
	errKeyInvalidProfessional   = "slot.error.invalid_professional"
	errKeyInvalidTimeRange      = "slot.error.invalid_time_range"
	errKeyInvalidMaxCapacity    = "slot.error.invalid_max_capacity"
	errKeyInvalidDate           = "slot.error.invalid_date"
	errKeyDateTimeInconsistency = "slot.error.date_time_inconsistency"
)

// validationErrorKey maps a NewSlot failure to the copy the user should see.
func validationErrorKey(err error) string {
	switch {
	case errors.Is(err, ErrInvalidProfessionalID):
		return errKeyInvalidProfessional
	case errors.Is(err, ErrInvalidTimeRange):
		return errKeyInvalidTimeRange
	case errors.Is(err, ErrInvalidMaxCapacity):
		return errKeyInvalidMaxCapacity
	case errors.Is(err, ErrInvalidDate):
		return errKeyInvalidDate
	case errors.Is(err, ErrDateTimeInconsistency):
		return errKeyDateTimeInconsistency
	default:
		return errKeyUnexpected
	}
}
