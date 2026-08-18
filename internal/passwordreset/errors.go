package passwordreset

import "errors"

var (
	ErrNilPgxPool       = errors.New("pgx pool cannot be nil")
	ErrUnknownAssistant = errors.New("assistant does not exist")

	ErrNilStorer      = errors.New("storer cannot be nil")
	ErrNonPositiveTTL = errors.New("token ttl must be positive")

	ErrNilAssistantID = errors.New("assistant id cannot be nil")

	ErrTokenNotFound = errors.New("password reset token not found")
	ErrTokenExpired  = errors.New("password reset token expired")
)
