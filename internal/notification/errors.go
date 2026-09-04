package notification

import "errors"

var (
	ErrNilLogger               = errors.New("logger cannot be nil")
	ErrNilSlotCancellationFunc = errors.New("slot cancellation lookup cannot be nil")
	ErrUnknownEventKind        = errors.New("unknown notification kind")
)
