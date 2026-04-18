package patient

import "errors"

var (
	ErrFirstNameRequired       = errors.New("first name is required")
	ErrLastNameRequired        = errors.New("last name is required")
	ErrPhoneRequired           = errors.New("phone number is required")
	ErrEmailRequired           = errors.New("email is required")
	ErrHealthInsuranceRequired = errors.New("health insurance is required")
	ErrInsuranceNumberRequired = errors.New("insurance number is required")
	ErrNilPatient              = errors.New("patient cannot be nil")

	ErrNilPgxPool             = errors.New("pgx pool cannot be nil")
	ErrInvalidHealthInsurance = errors.New("invalid health insurance")

	ErrNilLogger     = errors.New("logger cannot be nil")
	ErrNilRepository = errors.New("repository cannot be nil")
)
