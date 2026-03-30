package db

import "errors"

var (
	ErrNilContext             = errors.New("nil context")
	ErrEmptyDatabaseURL       = errors.New("empty database URL")
	ErrInvalidDatabaseURL     = errors.New("invalid database URL")
	ErrEmptyDatabaseURLScheme = errors.New("empty database URL scheme")
	ErrUnknownDatabaseScheme  = errors.New("unknown database URL scheme")
)
