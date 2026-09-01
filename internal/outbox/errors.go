package outbox

import "errors"

var (
	ErrNilTx              = errors.New("transaction cannot be nil")
	ErrEmptyAggregateType = errors.New("aggregate type cannot be empty")
	ErrNilAggregateID     = errors.New("aggregate ID cannot be nil")
	ErrEmptyEventType     = errors.New("event type cannot be empty")
	ErrPayloadNotObject   = errors.New("payload must encode to a JSON object")
)
