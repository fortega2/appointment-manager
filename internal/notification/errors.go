package notification

import "errors"

var (
	ErrNilLogger             = errors.New("logger cannot be nil")
	ErrInvalidTickerInterval = errors.New("ticker interval must be greater than zero")
	ErrInvalidBufferSize     = errors.New("buffer size must be greater than zero")

	ErrNilSlotCancellationFunc = errors.New("slot cancellation lookup cannot be nil")

	ErrUnknownEventKind = errors.New("unknown notification kind")
)
