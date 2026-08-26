package notification

import "uuid"

// EventKind identifies what happened, so a single queue can carry every kind of
// notification the service learns to send rather than growing one channel per
// kind.
type EventKind int16

const (
	// EventSlotCancelled announces that a slot was cancelled and the
	// appointments booked on it were called off with it.
	EventSlotCancelled EventKind = iota + 1
)

// String names the kind for instrumentation. The default is what keeps a kind
// nothing recognises from reaching a metric label as an unbounded value: every
// unhandled kind collapses onto one series rather than opening a new one.
func (k EventKind) String() string {
	switch k {
	case EventSlotCancelled:
		return "slot_cancelled"
	default:
		return "unknown"
	}
}

// Event is one queued notification. It carries identifiers rather than rendered
// content: recipients and wording are resolved when the event is sent, so an
// event that waited in the queue cannot deliver a stale view of the booking.
type Event struct {
	SlotID uuid.UUID
	Kind   EventKind
}
