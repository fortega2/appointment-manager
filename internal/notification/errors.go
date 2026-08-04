package notification

import "errors"

var (
	ErrNilLogger             = errors.New("logger cannot be nil")
	ErrInvalidTickerInterval = errors.New("ticker interval must be greater than zero")
	ErrInvalidBufferSize     = errors.New("buffer size must be greater than zero")

	ErrNilSlotCancellationFunc = errors.New("slot cancellation lookup cannot be nil")

	// ErrUnknownEventKind marks a queued event the drain has no sender for. It
	// is not returned to any caller -- the drain reports no errors -- but it
	// gives the send span a status and a message instead of ending silently on
	// a notification that was thrown away.
	ErrUnknownEventKind = errors.New("unknown notification kind")
)
