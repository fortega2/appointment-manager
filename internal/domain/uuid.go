package domain

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrInvalidID = errors.New("invalid id")

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
