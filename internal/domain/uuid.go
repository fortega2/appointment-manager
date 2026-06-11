package domain

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrInvalidID = errors.New("invalid id")

func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic("domain.NewID: " + err.Error())
	}
	return id
}

func NewIDString() string {
	return NewID().String()
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
