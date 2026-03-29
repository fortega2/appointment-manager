package server

import "errors"

var (
	ErrNilContext    = errors.New("server: nil context")
	ErrEmptyAddress  = errors.New("server: empty address")
	ErrNilLogger     = errors.New("server: nil logger")
	ErrNilHandler    = errors.New("server: nil handler")
	ErrInvalidConfig = errors.New("invalid server configuration")
)
