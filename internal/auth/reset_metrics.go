package auth

// ResetMetrics records the two facts the reset flow cannot show any other way:
// whether the mail left, and whether a password actually changed.
// *metrics.Metrics satisfies it structurally.
type ResetMetrics interface {
	RecordPasswordResetMailSent()
	RecordPasswordResetMailFailed()
	RecordPasswordResetCompleted()
}

// noopResetMetrics keeps the handler usable without a registry.
type noopResetMetrics struct{}

func (noopResetMetrics) RecordPasswordResetMailSent() {
	// no-op
}

func (noopResetMetrics) RecordPasswordResetMailFailed() {
	// no-op
}

func (noopResetMetrics) RecordPasswordResetCompleted() {
	// no-op
}
