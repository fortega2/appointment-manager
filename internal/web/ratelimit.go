package web

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	rateLimitLimitHeader     = "X-RateLimit-Limit"
	rateLimitRemainingHeader = "X-RateLimit-Remaining"
	rateLimitResetHeader     = "X-RateLimit-Reset"
	retryAfterHeader         = "Retry-After"
)

// SetRateLimitHeaders advertises how much quota the caller has left. reset is
// how long until the full allowance is back, written as delta-seconds rather
// than an epoch timestamp so it shares a unit with Retry-After and does not
// depend on the two clocks agreeing.
//
// These three fields are de-facto rather than standard: the IETF draft
// (draft-ietf-httpapi-ratelimit-headers) specifies RateLimit and
// RateLimit-Policy instead. See ADR 0009.
//
// Headers must be set before the status line is written, or net/http drops them.
func SetRateLimitHeaders(header http.Header, limit, remaining int, reset time.Duration) {
	header.Set(rateLimitLimitHeader, strconv.Itoa(max(limit, 0)))
	header.Set(rateLimitRemainingHeader, strconv.Itoa(max(remaining, 0)))
	header.Set(rateLimitResetHeader, strconv.FormatInt(deltaSeconds(reset), 10))
}

// SetRetryAfter writes Retry-After (RFC 9110 §10.2.3) in delta-seconds.
func SetRetryAfter(header http.Header, after time.Duration) {
	header.Set(retryAfterHeader, strconv.FormatInt(RetryAfterSeconds(after), 10))
}

// RetryAfterSeconds is how long a refused caller should wait, in whole seconds.
// It never returns zero: telling a caller it may retry immediately invites a
// retry that is certain to be refused again. Exported so a message shown to the
// user quotes the same number the header does.
func RetryAfterSeconds(after time.Duration) int64 {
	return max(deltaSeconds(after), 1)
}

// deltaSeconds rounds up, so a caller that waits exactly as long as it was told
// never comes back a fraction of a second too early.
func deltaSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}

	return int64(math.Ceil(d.Seconds()))
}
