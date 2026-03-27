package assistant

import (
	"fmt"

	"github.com/google/uuid"
)

type ID string

type Assistant struct {
	ID        ID     `json:"id"`
	Names     string `json:"names"`
	LastNames string `json:"last_names"`
	Email     string `json:"email"`
	Password  string `json:"-"`
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
