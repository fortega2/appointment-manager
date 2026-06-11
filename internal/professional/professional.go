package professional

import (
	"strings"

	"github.com/google/uuid"

	"appointment-manager/internal/domain"
)

type Professional struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Phone     string    `json:"phone"`
	Specialty string    `json:"specialty"`
	Active    bool      `json:"active"`
}

func NewProfessional(firstName, lastName, phone string) (*Professional, error) {
	if strings.TrimSpace(firstName) == "" {
		return nil, ErrFirstNameRequired
	}
	if strings.TrimSpace(lastName) == "" {
		return nil, ErrLastNameRequired
	}
	if strings.TrimSpace(phone) == "" {
		return nil, ErrPhoneRequired
	}

	return &Professional{
		ID:        domain.NewID(),
		FirstName: firstName,
		LastName:  lastName,
		Phone:     phone,
		Specialty: "kinesiology",
		Active:    true,
	}, nil
}

func (p *Professional) Update(firstName, lastName, phone string, active bool) error {
	if strings.TrimSpace(firstName) == "" {
		return ErrFirstNameRequired
	}
	if strings.TrimSpace(lastName) == "" {
		return ErrLastNameRequired
	}
	if strings.TrimSpace(phone) == "" {
		return ErrPhoneRequired
	}

	p.FirstName = firstName
	p.LastName = lastName
	p.Phone = phone
	p.Active = active

	return nil
}
