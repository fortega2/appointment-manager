package worker

import "errors"

var (
	ErrNilLogger             = errors.New("logger cannot be nil")
	ErrNilAppointmentExpirer = errors.New("appointment expirer cannot be nil")
	ErrInvalidTickerInterval = errors.New("ticker interval must be greater than zero")
)
