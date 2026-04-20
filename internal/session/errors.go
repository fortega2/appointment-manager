package session

import "errors"

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")

	ErrSessionNotInContext = errors.New("session not found in context")
)
