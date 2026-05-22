package patient

import (
	"strings"

	"github.com/google/uuid"
)

const maxInsuranceNumberLength = 11

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
	trimmedFirstName := strings.TrimSpace(firstName)
	if trimmedFirstName == "" {
		return nil, ErrFirstNameRequired
	}

	trimmedLastName := strings.TrimSpace(lastName)
	if trimmedLastName == "" {
		return nil, ErrLastNameRequired
	}

	trimmedPhone := strings.TrimSpace(phone)
	if trimmedPhone == "" {
		return nil, ErrPhoneRequired
	}

	parsedEmail := strings.TrimSpace(strings.ToLower(email))
	if parsedEmail == "" {
		return nil, ErrEmailRequired
	}

	if healthInsurance <= 0 {
		return nil, ErrHealthInsuranceRequired
	}

	trimmedInsuranceNumber := strings.TrimSpace(insuranceNumber)
	if trimmedInsuranceNumber == "" {
		return nil, ErrInsuranceNumberRequired
	}
	if len(trimmedInsuranceNumber) > maxInsuranceNumberLength {
		return nil, ErrInsuranceNumberTooLong
	}

	if clinicalNotes != nil {
		trimmedNotes := strings.TrimSpace(*clinicalNotes)
		if trimmedNotes != "" {
			clinicalNotes = &trimmedNotes
		} else {
			clinicalNotes = nil
		}
	}

	return &Patient{
		ID:              uuid.New(),
		FirstName:       trimmedFirstName,
		LastName:        trimmedLastName,
		Phone:           trimmedPhone,
		Email:           parsedEmail,
		HealthInsurance: healthInsurance,
		InsuranceNumber: trimmedInsuranceNumber,
		ClinicalNotes:   clinicalNotes,
	}, nil
}

func (p *Patient) Update(
	firstName, lastName, phone, email string,
	healthInsurance int,
	insuranceNumber string,
	clinicalNotes *string,
) error {
	trimmedFirstName := strings.TrimSpace(firstName)
	if trimmedFirstName == "" {
		return ErrFirstNameRequired
	}

	trimmedLastName := strings.TrimSpace(lastName)
	if trimmedLastName == "" {
		return ErrLastNameRequired
	}

	trimmedPhone := strings.TrimSpace(phone)
	if trimmedPhone == "" {
		return ErrPhoneRequired
	}

	parsedEmail := strings.TrimSpace(strings.ToLower(email))
	if parsedEmail == "" {
		return ErrEmailRequired
	}

	if healthInsurance <= 0 {
		return ErrHealthInsuranceRequired
	}

	trimmedInsuranceNumber := strings.TrimSpace(insuranceNumber)
	if trimmedInsuranceNumber == "" {
		return ErrInsuranceNumberRequired
	}

	if clinicalNotes != nil {
		trimmedNotes := strings.TrimSpace(*clinicalNotes)
		if trimmedNotes != "" {
			p.ClinicalNotes = &trimmedNotes
		} else {
			p.ClinicalNotes = nil
		}
	} else {
		p.ClinicalNotes = nil
	}

	p.FirstName = trimmedFirstName
	p.LastName = trimmedLastName
	p.Phone = trimmedPhone
	p.Email = parsedEmail
	p.HealthInsurance = healthInsurance
	p.InsuranceNumber = trimmedInsuranceNumber

	return nil
}

func ParseID(id string) (uuid.UUID, error) {
	return uuid.Parse(id)
}
