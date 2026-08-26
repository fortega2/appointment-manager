package domain

import (
	"errors"
	"fmt"
	"uuid"
)

var ErrInvalidID = errors.New("invalid id")

func NewID() uuid.UUID {
	return uuid.NewV7()
}

func NewIDString() string {
	return NewID().String()
}

func ParseID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil(), ErrInvalidID
	}
	parsedID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil(), fmt.Errorf("%w: %w", ErrInvalidID, err)
	}

	return parsedID, nil
}
