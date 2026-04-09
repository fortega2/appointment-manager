package professional

import "errors"

var (
	ErrFirstNameRequired = errors.New("first name is required")
	ErrLastNameRequired  = errors.New("last name is required")
	ErrPhoneRequired     = errors.New("phone number is required")
	ErrNilProfessional   = errors.New("professional cannot be nil")

	ErrNilLogger     = errors.New("logger cannot be nil")
	ErrNilRepository = errors.New("repository cannot be nil")

	ErrNilPgxPool                   = errors.New("pgx pool cannot be nil")
	ErrInvalidProfessionalSpecialty = errors.New("invalid professional specialty")
)
