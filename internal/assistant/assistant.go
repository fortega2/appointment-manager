package assistant

import (
	"fmt"
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
		return nil, ErrAssistantRequestFirstNameRequired
	}
	if strings.TrimSpace(lastName) == "" {
		return nil, ErrAssistantRequestLastNameRequired
	}
	if strings.TrimSpace(email) == "" {
		return nil, ErrAssistantRequestEmailRequired
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil, ErrAssistantRequestPasswordRequired
	}

	return &Assistant{
		ID:           uuid.New(),
		FirstName:    firstName,
		LastName:     lastName,
		Email:        email,
		PasswordHash: passwordHash,
	}, nil
}

func ParseID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, ErrInvalidID
	}
	parsedID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidID, err)
	}

	return parsedID, nil
}
