package auth

import "errors"

var (
	ErrNilLogger         = errors.New("logger cannot be nil")
	ErrNilSessionStore   = errors.New("session store cannot be nil")
	ErrNilAssistantRepo  = errors.New("assistant repository cannot be nil")
	ErrNilPasswordHasher = errors.New("password hasher cannot be nil")
)
