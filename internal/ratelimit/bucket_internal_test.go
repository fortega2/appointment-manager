package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bucketBurst  = 4
	bucketRefill = 30 * time.Second
)

var bucketReference = time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

func newFullBucket() *bucket {
	return &bucket{tokens: bucketBurst, last: bucketReference}
}

func bucketTestConfig() bucketConfig {
	return bucketConfig{burst: bucketBurst, refill: bucketRefill}
}

func TestBucketRefillEarnsOneTokenPerRefillInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		elapsed  time.Duration
		expected float64
	}{
		{name: "no time passed", elapsed: 0, expected: 0},
		{name: "half an interval", elapsed: bucketRefill / 2, expected: 0.5},
		{name: "one interval", elapsed: bucketRefill, expected: 1},
		{name: "three intervals", elapsed: 3 * bucketRefill, expected: 3},
		{name: "capped at burst", elapsed: 100 * bucketRefill, expected: bucketBurst},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := bucketTestConfig()
			drained := &bucket{tokens: 0, last: bucketReference}

			drained.refill(bucketReference.Add(tt.elapsed), cfg)

			assert.InDelta(t, tt.expected, drained.tokens, 0.0001)
		})
	}
}

func TestBucketRefillIgnoresTimeGoingBackwards(t *testing.T) {
	t.Parallel()

	cfg := bucketTestConfig()
	drained := &bucket{tokens: 1, last: bucketReference}

	drained.refill(bucketReference.Add(-time.Hour), cfg)

	assert.InDelta(t, 1.0, drained.tokens, 0.0001)
	assert.Equal(t, bucketReference, drained.last, "a backwards clock must not rewind the bucket")
}

func TestBucketDecideReportsHeaderValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		tokens        float64
		expectAllowed bool
		expectRemains int
		expectRetry   time.Duration
		expectReset   time.Duration
	}{
		{
			name:          "full bucket",
			tokens:        bucketBurst,
			expectAllowed: true,
			expectRemains: bucketBurst,
			expectRetry:   0,
			expectReset:   0,
		},
		{
			name:          "one token left",
			tokens:        1,
			expectAllowed: true,
			expectRemains: 1,
			expectRetry:   0,
			expectReset:   3 * bucketRefill,
		},
		{
			name:          "empty bucket",
			tokens:        0,
			expectAllowed: false,
			expectRemains: 0,
			expectRetry:   bucketRefill,
			expectReset:   bucketBurst * bucketRefill,
		},
		{
			name:          "partial token rounds the wait up",
			tokens:        0.25,
			expectAllowed: false,
			expectRemains: 0,
			expectRetry:   bucketRefill * 3 / 4,
			expectReset:   bucketRefill * 15 / 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := bucketTestConfig()
			tracked := &bucket{tokens: tt.tokens, last: bucketReference}

			decision := tracked.decide(bucketReference, cfg)

			assert.Equal(t, tt.expectAllowed, decision.Allowed)
			assert.Equal(t, bucketBurst, decision.Limit)
			assert.Equal(t, tt.expectRemains, decision.Remaining)
			assert.Equal(t, tt.expectRetry, decision.RetryAfter)
			assert.Equal(t, tt.expectReset, decision.Reset)
		})
	}
}

func TestBucketFillAndRefundStayWithinBurst(t *testing.T) {
	t.Parallel()

	cfg := bucketTestConfig()

	drained := &bucket{tokens: 0, last: bucketReference}
	drained.fill(cfg)
	assert.InDelta(t, float64(bucketBurst), drained.tokens, 0.0001)

	drained.refund(cfg)
	assert.InDelta(t, float64(bucketBurst), drained.tokens, 0.0001, "refund must not exceed the burst")

	spent := &bucket{tokens: 1, last: bucketReference}
	spent.refund(cfg)
	assert.InDelta(t, 2.0, spent.tokens, 0.0001)
}

func TestBucketFullOnlyOnceCompletelyRefilled(t *testing.T) {
	t.Parallel()

	cfg := bucketTestConfig()

	require.True(t, newFullBucket().full(bucketReference, cfg))

	drained := &bucket{tokens: 0, last: bucketReference}
	assert.False(t, drained.full(bucketReference, cfg))
	assert.False(t, drained.full(bucketReference.Add(bucketRefill), cfg), "one token back is not full")
	assert.True(t, drained.full(bucketReference.Add(bucketBurst*bucketRefill), cfg))
}
