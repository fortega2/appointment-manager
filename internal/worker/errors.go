package worker

import "errors"

var (
	ErrNilLogger             = errors.New("logger cannot be nil")
	ErrNilJob                = errors.New("job cannot be nil")
	ErrEmptyJobName          = errors.New("job name cannot be empty")
	ErrInvalidTickerInterval = errors.New("ticker interval must be greater than zero")
)
