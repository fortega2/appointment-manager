package patient

import "errors"

var (
	ErrFirstNameRequired       = errors.New("first name is required")
	ErrLastNameRequired        = errors.New("last name is required")
	ErrPhoneRequired           = errors.New("phone number is required")
	ErrEmailRequired           = errors.New("email is required")
	ErrHealthInsuranceRequired = errors.New("health insurance is required")
	ErrInsuranceNumberRequired = errors.New("insurance number is required")
	ErrInsuranceNumberTooLong  = errors.New("insurance number cannot be longer than 11 characters")
	ErrNilPatient              = errors.New("patient cannot be nil")

	ErrNilPgxPool             = errors.New("pgx pool cannot be nil")
	ErrInvalidHealthInsurance = errors.New("invalid health insurance")
	ErrPatientNotFound        = errors.New("patient not found")

	ErrNilLogger                    = errors.New("logger cannot be nil")
	ErrNilRepository                = errors.New("repository cannot be nil")
	ErrNilHealthInsuranceRepository = errors.New("health insurance repository cannot be nil")
)
