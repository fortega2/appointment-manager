package patient

import (
	"appointment-manager/internal/domain"
	"fmt"
)

type View struct {
	ID                  string
	FirstName           string
	LastName            string
	Phone               string
	Email               string
	HealthInsurance     int
	HealthInsuranceName string
	InsuranceNumber     string
	ClinicalNotes       string
}

func (v View) AlpineVisibility() string {
	return `searchQuery === '' || $el.dataset.search.toLowerCase().includes(searchQuery.toLowerCase())`
}

func (v View) ToPatient() (Patient, error) {
	ID, err := domain.ParseID(v.ID)
	if err != nil {
		return Patient{}, fmt.Errorf("parse patient id: %w", err)
	}

	return Patient{
		ID:              ID,
		FirstName:       v.FirstName,
		LastName:        v.LastName,
		Phone:           v.Phone,
		Email:           v.Email,
		HealthInsurance: v.HealthInsurance,
		InsuranceNumber: v.InsuranceNumber,
		ClinicalNotes:   &v.ClinicalNotes,
	}, nil
}
