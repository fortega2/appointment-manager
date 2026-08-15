package web_test

import (
	"appointment-manager/internal/web"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	limitHeader     = "X-RateLimit-Limit"
	remainingHeader = "X-RateLimit-Remaining"
	resetHeader     = "X-RateLimit-Reset"
	retryHeader     = "Retry-After"
)

func TestSetRateLimitHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		limit             int
		remaining         int
		reset             time.Duration
		expectedRemaining string
		expectedReset     string
	}{
		{
			name:              "full allowance",
			limit:             5,
			remaining:         5,
			reset:             0,
			expectedRemaining: "5",
			expectedReset:     "0",
		},
		{
			name:              "reset in whole seconds",
			limit:             5,
			remaining:         2,
			reset:             3 * time.Minute,
			expectedRemaining: "2",
			expectedReset:     "180",
		},
		{
			name:              "a fractional reset rounds up",
			limit:             5,
			remaining:         0,
			reset:             1500 * time.Millisecond,
			expectedRemaining: "0",
			expectedReset:     "2",
		},
		{
			name:              "a negative remaining is clamped",
			limit:             5,
			remaining:         -3,
			reset:             time.Second,
			expectedRemaining: "0",
			expectedReset:     "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}

			web.SetRateLimitHeaders(header, tt.limit, tt.remaining, tt.reset)

			assert.Equal(t, "5", header.Get(limitHeader))
			assert.Equal(t, tt.expectedRemaining, header.Get(remainingHeader))
			assert.Equal(t, tt.expectedReset, header.Get(resetHeader))
			assert.Empty(t, header.Get(retryHeader), "Retry-After belongs only on a refusal")
		})
	}
}

func TestSetRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		after    time.Duration
		expected string
	}{
		{name: "whole seconds", after: 3 * time.Minute, expected: "180"},
		{name: "rounds up", after: 2100 * time.Millisecond, expected: "3"},
		{name: "never advises an immediate retry", after: 0, expected: "1"},
		{name: "never advises a negative wait", after: -time.Second, expected: "1"},
		{name: "sub-second waits become one", after: 200 * time.Millisecond, expected: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}

			web.SetRetryAfter(header, tt.after)

			assert.Equal(t, tt.expected, header.Get(retryHeader))
		})
	}
}
