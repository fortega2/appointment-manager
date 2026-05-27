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

	ErrFirstNameRequired  = errors.New("first name is required")
	ErrLastNameRequired   = errors.New("last name is required")
	ErrEmailRequired      = errors.New("email is required")
	ErrEmailHasNoSign     = errors.New("email must contain '@' sign")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrPasswordRequired   = errors.New("password is required")
)
