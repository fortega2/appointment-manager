package assistant

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type ID string

type Assistant struct {
	ID           ID     `json:"id"`
	Names        string `json:"names"`
	LastNames    string `json:"last_names"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
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
		ID:           ID(uuid.NewString()),
		Names:        names,
		LastNames:    lastNames,
		Email:        email,
		PasswordHash: passwordHash,
	}, nil
}

func ParseID(raw string) (ID, error) {
	if raw == "" {
		return "", ErrInvalidID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidID, err)
	}
	return ID(raw), nil
}

func (id ID) String() string {
	return string(id)
}
