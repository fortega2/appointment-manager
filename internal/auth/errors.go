package auth

import "errors"

var (
	ErrNilLogger         = errors.New("logger cannot be nil")
	ErrNilSessionStore   = errors.New("session store cannot be nil")
	ErrNilAssistantRepo  = errors.New("assistant repository cannot be nil")
	ErrNilPasswordHasher = errors.New("password hasher cannot be nil")
	ErrNilRateLimiter    = errors.New("rate limiter cannot be nil")
)

var (
	ErrNilResetTokenStore = errors.New("reset token store cannot be nil")
	ErrNilMailer          = errors.New("mailer cannot be nil")
	ErrEmptyBaseURL       = errors.New("base url cannot be empty")
)

// Outcomes of verifyCredentials. errInvalidCredentials covers both an unknown
// email and a wrong password, so callers cannot leak account existence; the
// other two separate infrastructure failures from a rejected login.
var (
	errInvalidCredentials     = errors.New("invalid credentials")
	errCredentialLookupFailed = errors.New("failed to look up credentials")
	errPasswordCheckFailed    = errors.New("failed to check password")
)
