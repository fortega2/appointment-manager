package assistant

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Assistant struct {
	ID           uuid.UUID `json:"id"`
	Names        string    `json:"names"`
	LastNames    string    `json:"last_names"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
}

func NewAssistant(names, lastNames, email, passwordHash string) (*Assistant, error) {
	if strings.TrimSpace(names) == "" {
		return nil, ErrAssistantRequestNamesRequired
	}
	if strings.TrimSpace(lastNames) == "" {
		return nil, ErrAssistantRequestLastNamesRequired
	}
	if strings.TrimSpace(email) == "" {
		return nil, ErrAssistantRequestEmailRequired
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil, ErrAssistantRequestPasswordRequired
	}

	return &Assistant{
		ID:           uuid.New(),
		Names:        names,
		LastNames:    lastNames,
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
