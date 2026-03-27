package assistant

import "errors"

var (
	ErrAssistantNotFound = errors.New("assistant not found")
	ErrInvalidID         = errors.New("invalid assistant id")

	ErrNilLogger     = errors.New("logger cannot be nil")
	ErrNilRepository = errors.New("repository cannot be nil")
)
