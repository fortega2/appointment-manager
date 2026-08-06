package session

import "errors"

var (
	ErrNilPgxPool         = errors.New("pgx pool cannot be nil")
	ErrInvalidAssistantID = errors.New("assistant id is not a valid uuid")
	ErrUnknownAssistant   = errors.New("assistant does not exist")

	ErrNilStorer = errors.New("storer cannot be nil")

	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")

	ErrSessionNotInContext = errors.New("session not found in context")
)
