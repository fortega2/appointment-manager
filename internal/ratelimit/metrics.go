package ratelimit

// Metrics records the rejections a limiter hands out and the entries it drops
// to stay within its cap. Declared here so the package depends on nothing but
// the standard library; *metrics.Metrics satisfies it structurally.
type Metrics interface {
	RecordLoginRateLimitedByAccount()
	RecordLoginRateLimitedByIP()
	RecordLoginRateLimitEvicted()
}

// noopMetrics keeps Limiter usable without a registry.
type noopMetrics struct{}

func (noopMetrics) RecordLoginRateLimitedByAccount() {
	// no-op
}

func (noopMetrics) RecordLoginRateLimitedByIP() {
	// no-op
}

func (noopMetrics) RecordLoginRateLimitEvicted() {
	// no-op
}
