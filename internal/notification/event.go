package notification

import "uuid"

// EventKind identifies what happened.
type EventKind int16

const (
	// EventSlotCancelled announces that a slot was cancelled and the
	// appointments booked on it were called off with it.
	EventSlotCancelled EventKind = iota + 1
)

// String names the kind for instrumentation. The default collapses unhandled
// kinds onto one series rather than opening an unbounded metric label.
func (k EventKind) String() string {
	switch k {
	case EventSlotCancelled:
		return "slot_cancelled"
	default:
		return "unknown"
	}
}

// Event is one notification. It carries identifiers rather than rendered
// content: recipients and wording are resolved at send time.
type Event struct {
	SlotID uuid.UUID
	Kind   EventKind
}
