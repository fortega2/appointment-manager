package outbox

import "errors"

var (
	ErrNilTx              = errors.New("transaction cannot be nil")
	ErrEmptyAggregateType = errors.New("aggregate type cannot be empty")
	ErrNilAggregateID     = errors.New("aggregate ID cannot be nil")
	ErrEmptyEventType     = errors.New("event type cannot be empty")
	ErrPayloadNotObject   = errors.New("payload must encode to a JSON object")

	ErrNilLogger           = errors.New("logger cannot be nil")
	ErrNilPool             = errors.New("pgx pool cannot be nil")
	ErrInvalidBatchSize    = errors.New("batch size must be greater than zero")
	ErrNilHandler          = errors.New("handler cannot be nil")
	ErrDuplicateHandler    = errors.New("handler already registered for this event type")
	ErrNoHandlerRegistered = errors.New("no handler registered for this event type")
)
