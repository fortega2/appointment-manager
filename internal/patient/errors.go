package patient

import (
	"errors"
	"fmt"
)

// Length limits are interpolated from the constants they enforce so the message
// cannot drift from the rule.
var (
	ErrFirstNameRequired            = errors.New("first name is required")
	ErrFirstNameTooLong             = fmt.Errorf("first name cannot be longer than %d characters", maxNameLength)
	ErrLastNameRequired             = errors.New("last name is required")
	ErrLastNameTooLong              = fmt.Errorf("last name cannot be longer than %d characters", maxNameLength)
	ErrPhoneRequired                = errors.New("phone number is required")
	ErrEmailRequired                = errors.New("email is required")
	ErrEmailTooLong                 = fmt.Errorf("email cannot be longer than %d characters", maxEmailLength)
	ErrHealthInsuranceRequired      = errors.New("health insurance is required")
	ErrInsuranceNumberRequired      = errors.New("insurance number is required")
	ErrInvalidInsuranceNumberLength = fmt.Errorf("insurance number must be exactly %d characters", insuranceNumberLength)
	ErrNilPatient                   = errors.New("patient cannot be nil")

	ErrNilPgxPool             = errors.New("pgx pool cannot be nil")
	ErrInvalidHealthInsurance = errors.New("invalid health insurance")
	ErrPatientNotFound        = errors.New("patient not found")

	ErrNilLogger                    = errors.New("logger cannot be nil")
	ErrNilRepository                = errors.New("repository cannot be nil")
	ErrNilHealthInsuranceRepository = errors.New("health insurance repository cannot be nil")
)
