package password

import (
	"context"
	"time"
)

// Metrics records how long callers wait for a hashing slot, including the ones
// that give up. Declared here so the package depends on nothing but the
// standard library; *metrics.Metrics satisfies it structurally.
type Metrics interface {
	ObservePasswordQueueWait(ctx context.Context, waited time.Duration)
	RecordPasswordQueueTimedOut()
	RecordPasswordQueueClientCancelled()
}

// noopMetrics keeps Argon2 usable without a registry.
type noopMetrics struct{}

func (noopMetrics) ObservePasswordQueueWait(context.Context, time.Duration) {
	// no-op
}

func (noopMetrics) RecordPasswordQueueTimedOut() {
	// no-op
}

func (noopMetrics) RecordPasswordQueueClientCancelled() {
	// no-op
}
