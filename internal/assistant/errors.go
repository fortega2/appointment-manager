package assistant

import "errors"

var (
	ErrAssistantNotFound = errors.New("assistant not found")
	ErrInvalidID         = errors.New("invalid assistant id")

	ErrNilLogger         = errors.New("logger cannot be nil")
	ErrNilRepository     = errors.New("repository cannot be nil")
	ErrNilPasswordHasher = errors.New("password hasher cannot be nil")
	ErrNilService        = errors.New("service cannot be nil")
	ErrNilPgxPool        = errors.New("pgx pool cannot be nil")

	ErrEmptyPasswordHash = errors.New("password hash cannot be empty")

	ErrAssistantRequestFirstNameRequired = errors.New("names is required")
	ErrAssistantRequestLastNameRequired  = errors.New("last names is required")
	ErrAssistantRequestEmailRequired     = errors.New("email is required")
	ErrAssistantRequestPasswordRequired  = errors.New("password is required")
)
