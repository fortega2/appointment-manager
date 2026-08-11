package password

import (
	"context"
	"time"
)

// client_cancelled is the caller's own context ending first, so it says nothing
// about load and must not be read as saturation.
const (
	waitFailureTimeout         = "timeout"
	waitFailureClientCancelled = "client_cancelled"
)

// Metrics records how long callers wait for a hashing slot. Declared here so
// the package depends on nothing but the standard library; *metrics.Metrics
// satisfies it structurally.
type Metrics interface {
	ObservePasswordQueueWait(ctx context.Context, waited time.Duration)
	RecordPasswordQueueTimeout(reason string)
}

// noopMetrics keeps Argon2 usable without a registry.
type noopMetrics struct{}

func (noopMetrics) ObservePasswordQueueWait(context.Context, time.Duration) {
	// no-op
}

func (noopMetrics) RecordPasswordQueueTimeout(string) {
	// no-op
}
