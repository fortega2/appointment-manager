package worker

import "errors"

var (
	ErrNilLogger             = errors.New("logger cannot be nil")
	ErrNilJob                = errors.New("job cannot be nil")
	ErrEmptyJobName          = errors.New("job name cannot be empty")
	ErrDuplicateJobName      = errors.New("job name is already registered")
	ErrGroupStarted          = errors.New("jobs cannot be added after the group started")
	ErrInvalidTickerInterval = errors.New("ticker interval must be greater than zero")
	ErrInvalidJobTimeout     = errors.New("job timeout must be greater than zero")
)
