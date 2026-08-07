package patient

import (
	"errors"

	"appointment-manager/internal/i18n"
)

// Catalog keys for the copy this package shows the user. The Go errors keep
// their English text for the logs; these are what reaches the screen.
const (
	msgKeyCreated = "patient.message.created"
	msgKeyUpdated = "patient.message.updated"

	errKeyLoadPatients                 = "patient.error.load_patients"
	errKeyLoadInsurances               = "patient.error.load_insurances"
	errKeyLoadPatient                  = "patient.error.load_patient"
	errKeyProcessPatient               = "patient.error.process_patient"
	errKeyParseForm                    = "patient.error.parse_form"
	errKeyMissingID                    = "patient.error.missing_id"
	errKeyInvalidID                    = "patient.error.invalid_id"
	errKeyInvalidInsurance             = "patient.error.invalid_insurance"
	errKeyNotFound                     = "patient.error.not_found"
	errKeyCreate                       = "patient.error.create"
	errKeyUpdate                       = "patient.error.update"
	errKeyUnexpected                   = "common.error.unexpected"
	errKeyFirstNameRequired            = "patient.error.first_name_required"
	errKeyFirstNameTooLong             = "patient.error.first_name_too_long"
	errKeyLastNameRequired             = "patient.error.last_name_required"
	errKeyLastNameTooLong              = "patient.error.last_name_too_long"
	errKeyPhoneRequired                = "patient.error.phone_required"
	errKeyEmailRequired                = "patient.error.email_required"
	errKeyEmailTooLong                 = "patient.error.email_too_long"
	errKeyHealthInsuranceRequired      = "patient.error.health_insurance_required"
	errKeyInsuranceNumberRequired      = "patient.error.insurance_number_required"
	errKeyInvalidInsuranceNumberLength = "patient.error.insurance_number_length"
)

// validationErrorKey maps a domain validation failure to a catalog key and the
// values its placeholders need.
func validationErrorKey(err error) (string, i18n.M) {
	switch {
	case errors.Is(err, ErrFirstNameRequired):
		return errKeyFirstNameRequired, nil
	case errors.Is(err, ErrFirstNameTooLong):
		return errKeyFirstNameTooLong, i18n.M{"count": maxNameLength}
	case errors.Is(err, ErrLastNameRequired):
		return errKeyLastNameRequired, nil
	case errors.Is(err, ErrLastNameTooLong):
		return errKeyLastNameTooLong, i18n.M{"count": maxNameLength}
	case errors.Is(err, ErrPhoneRequired):
		return errKeyPhoneRequired, nil
	case errors.Is(err, ErrEmailRequired):
		return errKeyEmailRequired, nil
	case errors.Is(err, ErrEmailTooLong):
		return errKeyEmailTooLong, i18n.M{"count": maxEmailLength}
	case errors.Is(err, ErrHealthInsuranceRequired):
		return errKeyHealthInsuranceRequired, nil
	case errors.Is(err, ErrInsuranceNumberRequired):
		return errKeyInsuranceNumberRequired, nil
	case errors.Is(err, ErrInvalidInsuranceNumberLength):
		return errKeyInvalidInsuranceNumberLength, i18n.M{"count": insuranceNumberLength}
	default:
		return errKeyUnexpected, nil
	}
}
