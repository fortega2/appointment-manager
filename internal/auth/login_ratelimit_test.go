package auth

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/password"
	"appointment-manager/internal/ratelimit"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	limitedEmail    = "assistant@example.com"
	limitedPassword = "whatever"
	// The address httptest gives every request it builds.
	limitedAddr = "192.0.2.1"

	rateLimitedES = "Demasiados intentos. Probá de nuevo en 60 segundos"
	rateLimitedEN = "Too many attempts. Try again in 60 seconds"

	rateLimitedOnceES = "Demasiados intentos. Probá de nuevo en 1 segundo"
	rateLimitedOnceEN = "Too many attempts. Try again in 1 second"

	limitHeader     = "X-RateLimit-Limit"
	remainingHeader = "X-RateLimit-Remaining"
	resetHeader     = "X-RateLimit-Reset"
	retryHeader     = "Retry-After"
	redirectHeader  = "HX-Redirect"
)

// newSpentLimiter returns a limiter whose single account token has already been
// spent, so the next attempt is refused before it reaches the password check.
// Draining it up front is what keeps these tests off the database: the refusal
// returns before verifyCredentials is ever called.
func newSpentLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()

	limiter := newLimiterWithBurst(t, 1)
	require.True(t, limiter.Allow(netip.MustParseAddr(limitedAddr), limitedEmail).Allowed)

	return limiter
}

func newSpentLimiterMux(t *testing.T) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()
	newTestHandlerWithLimiter(t, password.NewArgon2(nil), newSpentLimiter(t)).RegisterHandlers(mux)

	return mux
}

func TestLoginAPIRefusesAnAttemptOverTheAllowance(t *testing.T) {
	t.Parallel()

	mux := newSpentLimiterMux(t)

	body := `{"email":"` + limitedEmail + `","password":"` + limitedPassword + `"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, authLoginPath, strings.NewReader(body))
	req.Header.Set(authContentTypeHeader, authJSONType)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get(authContentTypeHeader))
	assert.Equal(t, "60", rec.Header().Get(retryHeader))
	assert.Equal(t, "1", rec.Header().Get(limitHeader))
	assert.Equal(t, "0", rec.Header().Get(remainingHeader))
	assert.Equal(t, "60", rec.Header().Get(resetHeader))

	var problem struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, "/problems/too-many-requests", problem.Type)
	assert.Equal(t, http.StatusTooManyRequests, problem.Status)
	assert.NotContains(t, problem.Detail, limitedEmail, "the refusal must not echo the account back")
}

func TestLoginUIRefusalFollowsTheRequestLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		locale i18n.Locale
		want   string
	}{
		{name: "spanish", locale: i18n.LocaleES, want: rateLimitedES},
		{name: "english", locale: i18n.LocaleEN, want: rateLimitedEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := newSpentLimiterMux(t)

			form := url.Values{"email": {limitedEmail}, "password": {limitedPassword}}
			ctx := i18n.WithLocale(t.Context(), tt.locale)
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, loginUIPath, strings.NewReader(form.Encode()))
			req.Header.Set(headerContentTyp, formContentType)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			require.Equal(t, http.StatusTooManyRequests, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
			assert.Equal(t, "60", rec.Header().Get(retryHeader),
				"the header and the copy must quote the same number")
			assert.Empty(t, rec.Header().Get(redirectHeader), "a refusal must not redirect")
		})
	}
}

func TestLoginRefusalIsSharedAcrossBothTransports(t *testing.T) {
	t.Parallel()

	mux := newSpentLimiterMux(t)

	// Spending the allowance through one transport must close the other too, or
	// alternating between them would buy twice the budget.
	form := url.Values{"email": {limitedEmail}, "password": {limitedPassword}}
	uiReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, loginUIPath, strings.NewReader(form.Encode()))
	uiReq.Header.Set(headerContentTyp, formContentType)
	uiRec := httptest.NewRecorder()
	mux.ServeHTTP(uiRec, uiReq)

	body := `{"email":"` + limitedEmail + `","password":"` + limitedPassword + `"}`
	apiReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, authLoginPath, strings.NewReader(body))
	apiReq.Header.Set(authContentTypeHeader, authJSONType)
	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, apiReq)

	assert.Equal(t, http.StatusTooManyRequests, uiRec.Code)
	assert.Equal(t, http.StatusTooManyRequests, apiRec.Code)
}

func TestLoginAccountKeyIgnoresCaseAndPadding(t *testing.T) {
	t.Parallel()

	mux := newSpentLimiterMux(t)

	body := `{"email":"  ASSISTANT@Example.COM ","password":"` + limitedPassword + `"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, authLoginPath, strings.NewReader(body))
	req.Header.Set(authContentTypeHeader, authJSONType)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code,
		"recasing the address must not buy a second allowance")
}

// TestRefundOnlyHappensWhenTheFailureCarriesNoCredentialSignal reaches for the
// unexported helper on purpose: the failures it discriminates come from a nil
// database pool and a saturated Argon2 semaphore, neither of which a handler
// test can produce without a real dependency.
func TestRefundOnlyHappensWhenTheFailureCarriesNoCredentialSignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		refunded bool
	}{
		{"a wrong password is what the allowance rations", errInvalidCredentials, false},
		{"a wrapped wrong password still counts", fmt.Errorf("wrapped: %w", errInvalidCredentials), false},
		{"a lookup that never ran is our outage", errCredentialLookupFailed, true},
		{"a hashing slot that was never free is our outage", password.ErrTooManyConcurrentHashes, true},
		{"a hash comparison that broke is our outage", errPasswordCheckFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limiter := newLimiterWithBurst(t, 1)
			addr := netip.MustParseAddr(limitedAddr)
			require.True(t, limiter.Allow(addr, limitedEmail).Allowed)
			require.False(t, limiter.Allow(addr, limitedEmail).Allowed, "the budget must start spent")

			h := newTestHandlerWithLimiter(t, password.NewArgon2(nil), limiter)
			rec := httptest.NewRecorder()

			h.refundUnlessCredentialFailure(rec, addr, limitedEmail, tt.err)

			assert.Equal(t, tt.refunded, limiter.Allow(addr, limitedEmail).Allowed)
			if tt.refunded {
				assert.Equal(t, "1", rec.Header().Get(limitHeader), "the headers must describe the state after the refund")
			} else {
				assert.Empty(t, rec.Header().Get(limitHeader), "an attempt that was rationed rewrites nothing")
			}
		})
	}
}

// newOneSecondLimiterMux drains a limiter whose account token comes back after a
// second, which is what puts RetryAfterSeconds at its clamped minimum of one —
// the case that used to render "1 segundos".
func newOneSecondLimiterMux(t *testing.T) *http.ServeMux {
	t.Helper()

	limiter, err := ratelimit.New(ratelimit.Config{
		Enabled:       true,
		AccountBurst:  1,
		AccountRefill: time.Second,
		IPBurst:       roomyBurst,
		IPRefill:      limiterRefill,
		MaxEntries:    limiterMaxEntries,
	}, nil)
	require.NoError(t, err)
	require.True(t, limiter.Allow(netip.MustParseAddr(limitedAddr), limitedEmail).Allowed)

	mux := http.NewServeMux()
	newTestHandlerWithLimiter(t, password.NewArgon2(nil), limiter).RegisterHandlers(mux)

	return mux
}

func TestLoginUIRefusalReadsAsASingularSecond(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		locale i18n.Locale
		want   string
	}{
		{name: "spanish", locale: i18n.LocaleES, want: rateLimitedOnceES},
		{name: "english", locale: i18n.LocaleEN, want: rateLimitedOnceEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := newOneSecondLimiterMux(t)

			form := url.Values{"email": {limitedEmail}, "password": {limitedPassword}}
			ctx := i18n.WithLocale(t.Context(), tt.locale)
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, loginUIPath, strings.NewReader(form.Encode()))
			req.Header.Set(headerContentTyp, formContentType)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			require.Equal(t, http.StatusTooManyRequests, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
			assert.Equal(t, "1", rec.Header().Get(retryHeader),
				"the header and the copy must quote the same number")
		})
	}
}
