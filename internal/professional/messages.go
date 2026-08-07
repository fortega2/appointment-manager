package professional

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

	specialtyKinesiology    = "kinesiology"
	specialtyKeyKinesiology = "professional.specialty.kinesiology"
)

func specialtyLabelKey(specialty string) (string, bool) {
	switch specialty {
	case specialtyKinesiology:
		return specialtyKeyKinesiology, true
	default:
		return "", false
	}
}
