package ratelimit

import (
	"math"
	"time"
)

// bucketConfig is one bucket's shape: how many tokens it holds and how long it
// takes to earn one back.
type bucketConfig struct {
	burst  float64
	refill time.Duration
}

// durationFor is how long accruing the given number of tokens takes. It rounds
// up, so a caller told to wait never comes back a hair too early.
func (c bucketConfig) durationFor(tokens float64) time.Duration {
	if tokens <= 0 {
		return 0
	}

	return time.Duration(math.Ceil(tokens * float64(c.refill)))
}

// bucket is a token bucket: a count and the instant it was last brought up to
// date. It is two words per tracked key no matter how much traffic that key
// sees, which is what keeps the store's memory bounded by its entry cap rather
// than by whatever an attacker chooses to send.
type bucket struct {
	tokens float64
	last   time.Time
}

// refill brings the bucket up to now. It is idempotent for a given now, so
// deciding and then re-reading the bucket costs nothing extra.
func (b *bucket) refill(now time.Time, cfg bucketConfig) {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}

	b.tokens = min(cfg.burst, b.tokens+float64(elapsed)/float64(cfg.refill))
	b.last = now
}

// headroom reports what the bucket has left, carrying no verdict of its own.
// It is separate from decide because a bucket whose last token has just been
// spent still granted the request that spent it: re-deriving the verdict from
// the state after consuming would refuse that request.
func (b *bucket) headroom(now time.Time, cfg bucketConfig) Decision {
	b.refill(now, cfg)

	return Decision{
		Allowed:   true,
		Limit:     int(cfg.burst),
		Remaining: int(b.tokens),
		Reset:     cfg.durationFor(cfg.burst - b.tokens),
	}
}

// decide reports whether a token is available, along with the numbers the
// RateLimit headers carry. It does not consume: consumption is a separate step
// because a request is charged only once every bucket it touches has agreed.
func (b *bucket) decide(now time.Time, cfg bucketConfig) Decision {
	decision := b.headroom(now, cfg)
	if b.tokens < 1 {
		decision.Allowed = false
		decision.RetryAfter = cfg.durationFor(1 - b.tokens)
	}

	return decision
}

func (b *bucket) consume() {
	b.tokens--
}

// fill puts the bucket back to full, which is what a proven-legitimate caller
// earns. Without it, five typos would leave the account on a token every few
// minutes.
func (b *bucket) fill(cfg bucketConfig) {
	b.tokens = cfg.burst
}

// refund returns a single token, undoing one consume.
func (b *bucket) refund(cfg bucketConfig) {
	b.tokens = min(cfg.burst, b.tokens+1)
}

// full reports whether the bucket has nothing left to remember: a caller who
// gets a fresh entry is in exactly the same position as one who finds this, so
// the sweep can drop it.
func (b *bucket) full(now time.Time, cfg bucketConfig) bool {
	b.refill(now, cfg)

	return b.tokens >= cfg.burst
}
