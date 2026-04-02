package assistant

import (
	"strings"

	"github.com/google/uuid"
)

type Assistant struct {
	ID           uuid.UUID `json:"id"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
}

func NewAssistant(firstName, lastName, email, passwordHash string) (*Assistant, error) {
	if strings.TrimSpace(firstName) == "" {
		return nil, ErrFirstNameRequired
	}
	if strings.TrimSpace(lastName) == "" {
		return nil, ErrLastNameRequired
	}
	if err := isValidEmail(email); err != nil {
		return nil, err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil, ErrPasswordRequired
	}
	loweredEmail := strings.ToLower(email)

	return &Assistant{
		ID:           uuid.New(),
		FirstName:    firstName,
		LastName:     lastName,
		Email:        loweredEmail,
		PasswordHash: passwordHash,
	}, nil
}

func isValidEmail(email string) error {
	if strings.TrimSpace(email) == "" {
		return ErrEmailRequired
	}
	if !strings.Contains(email, "@") {
		return ErrEmailHasNoSign
	}
	return nil
}
