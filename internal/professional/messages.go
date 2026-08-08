package professional

import "errors"

// Catalog keys for the copy this package shows the user. The Go errors keep
// their English text for the logs; these are what reaches the screen.
const (
	msgKeyCreated = "professional.message.created"
	msgKeyUpdated = "professional.message.updated"

	errKeyLoadProfessionals = "professional.error.load_professionals"
	errKeyLoadProfessional  = "professional.error.load_professional"
	errKeyParseForm         = "professional.error.parse_form"
	errKeyMissingID         = "professional.error.missing_id"
	errKeyInvalidID         = "professional.error.invalid_id"
	errKeyNotFound          = "professional.error.not_found"
	errKeyCreate            = "professional.error.create"
	errKeyUpdate            = "professional.error.update"

	errKeyFirstNameRequired = "professional.error.first_name_required"
	errKeyLastNameRequired  = "professional.error.last_name_required"
	errKeyPhoneRequired     = "professional.error.phone_required"

	specialtyKinesiology    = "kinesiology"
	specialtyKeyKinesiology = "professional.specialty.kinesiology"
)

func validationErrorKey(err error, fallback string) string {
	switch {
	case errors.Is(err, ErrFirstNameRequired):
		return errKeyFirstNameRequired
	case errors.Is(err, ErrLastNameRequired):
		return errKeyLastNameRequired
	case errors.Is(err, ErrPhoneRequired):
		return errKeyPhoneRequired
	default:
		return fallback
	}
}

func specialtyLabelKey(specialty string) (string, bool) {
	switch specialty {
	case specialtyKinesiology:
		return specialtyKeyKinesiology, true
	default:
		return "", false
	}
}
