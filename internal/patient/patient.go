package patient

import (
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"appointment-manager/internal/domain"
)

const (
	insuranceNumberLength = 11
	maxNameLength         = 255
	maxEmailLength        = 255
)

type Patient struct {
	ID              uuid.UUID `json:"id"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	Phone           string    `json:"phone"`
	Email           string    `json:"email"`
	HealthInsurance int       `json:"health_insurance"`
	InsuranceNumber string    `json:"insurance_number"`
	ClinicalNotes   *string   `json:"clinical_notes,omitempty"`
}

func NewPatient(
	firstName, lastName, phone, email string,
	healthInsurance int,
	insuranceNumber string,
	clinicalNotes *string,
) (*Patient, error) {
	fields, err := newPatientFields(firstName, lastName, phone, email, healthInsurance, insuranceNumber, clinicalNotes)
	if err != nil {
		return nil, err
	}

	return &Patient{
		ID:              domain.NewID(),
		FirstName:       fields.firstName,
		LastName:        fields.lastName,
		Phone:           fields.phone,
		Email:           fields.email,
		HealthInsurance: fields.healthInsurance,
		InsuranceNumber: fields.insuranceNumber,
		ClinicalNotes:   fields.clinicalNotes,
	}, nil
}

func (p *Patient) Update(
	firstName, lastName, phone, email string,
	healthInsurance int,
	insuranceNumber string,
	clinicalNotes *string,
) error {
	fields, err := newPatientFields(firstName, lastName, phone, email, healthInsurance, insuranceNumber, clinicalNotes)
	if err != nil {
		return err
	}

	p.FirstName = fields.firstName
	p.LastName = fields.lastName
	p.Phone = fields.phone
	p.Email = fields.email
	p.HealthInsurance = fields.healthInsurance
	p.InsuranceNumber = fields.insuranceNumber
	p.ClinicalNotes = fields.clinicalNotes

	return nil
}

type patientFields struct {
	firstName       string
	lastName        string
	phone           string
	email           string
	healthInsurance int
	insuranceNumber string
	clinicalNotes   *string
}

// newPatientFields is the single definition of what makes a patient valid:
// NewPatient and Update both go through it, so an edited record cannot end up
// held to weaker rules than a newly created one.
func newPatientFields(
	firstName, lastName, phone, email string,
	healthInsurance int,
	insuranceNumber string,
	clinicalNotes *string,
) (patientFields, error) {
	trimmedFirstName := strings.TrimSpace(firstName)
	if trimmedFirstName == "" {
		return patientFields{}, ErrFirstNameRequired
	}
	if utf8.RuneCountInString(trimmedFirstName) > maxNameLength {
		return patientFields{}, ErrFirstNameTooLong
	}

	trimmedLastName := strings.TrimSpace(lastName)
	if trimmedLastName == "" {
		return patientFields{}, ErrLastNameRequired
	}
	if utf8.RuneCountInString(trimmedLastName) > maxNameLength {
		return patientFields{}, ErrLastNameTooLong
	}

	trimmedPhone := strings.TrimSpace(phone)
	if trimmedPhone == "" {
		return patientFields{}, ErrPhoneRequired
	}

	parsedEmail := strings.TrimSpace(strings.ToLower(email))
	if parsedEmail == "" {
		return patientFields{}, ErrEmailRequired
	}
	if utf8.RuneCountInString(parsedEmail) > maxEmailLength {
		return patientFields{}, ErrEmailTooLong
	}

	if healthInsurance <= 0 {
		return patientFields{}, ErrHealthInsuranceRequired
	}

	trimmedInsuranceNumber := strings.TrimSpace(insuranceNumber)
	if trimmedInsuranceNumber == "" {
		return patientFields{}, ErrInsuranceNumberRequired
	}
	if utf8.RuneCountInString(trimmedInsuranceNumber) != insuranceNumberLength {
		return patientFields{}, ErrInvalidInsuranceNumberLength
	}

	if clinicalNotes != nil {
		trimmedNotes := strings.TrimSpace(*clinicalNotes)
		if trimmedNotes != "" {
			clinicalNotes = &trimmedNotes
		} else {
			clinicalNotes = nil
		}
	}

	return patientFields{
		firstName:       trimmedFirstName,
		lastName:        trimmedLastName,
		phone:           trimmedPhone,
		email:           parsedEmail,
		healthInsurance: healthInsurance,
		insuranceNumber: trimmedInsuranceNumber,
		clinicalNotes:   clinicalNotes,
	}, nil
}
