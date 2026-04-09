package professional

import (
	"strings"

	"github.com/google/uuid"
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
		ID:        uuid.New(),
		FirstName: firstName,
		LastName:  lastName,
		Phone:     phone,
		Specialty: "kinesiology",
		Active:    true,
	}, nil
}
