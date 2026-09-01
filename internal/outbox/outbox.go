// Package outbox records events in the same transaction as the domain change
// that produced them. It only writes; draining public.outbox belongs to the
// consumer.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const insertEventQuery = `
	INSERT INTO public.outbox (
		aggregate_type,
		aggregate_id,
		event_type,
		payload
	) VALUES ($1, $2, $3, $4)
`

var emptyPayload = []byte(`{}`)

// AggregateType names the kind of entity an event is about. Producers declare their own values.
type AggregateType string

// EventType names what happened. Consumers dispatch on it.
type EventType string

// Event is one row of the outbox. Payload carries identifiers, never resolved
// personal data, and nil stores an empty object.
type Event struct {
	Payload       any
	AggregateType AggregateType
	EventType     EventType
	AggregateID   uuid.UUID
}

// Insert writes event through tx, so it commits with the change that caused it
// or not at all.
func Insert(ctx context.Context, tx pgx.Tx, event Event) error {
	if tx == nil {
		return ErrNilTx
	}

	if event.AggregateType == "" {
		return ErrEmptyAggregateType
	}

	if event.AggregateID == uuid.Nil() {
		return ErrNilAggregateID
	}

	if event.EventType == "" {
		return ErrEmptyEventType
	}

	payload, err := encodePayload(event.Payload)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		ctx,
		insertEventQuery,
		string(event.AggregateType),
		event.AggregateID,
		string(event.EventType),
		payload,
	); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
}

// encodePayload marshals payload and rejects anything that is not a JSON object
// here, because chk_outbox_payload_object would abort the caller's whole
// transaction instead.
func encodePayload(payload any) ([]byte, error) {
	if payload == nil {
		return emptyPayload, nil
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode outbox payload: %w", err)
	}

	if len(encoded) == 0 || encoded[0] != '{' {
		return nil, ErrPayloadNotObject
	}

	return encoded, nil
}
