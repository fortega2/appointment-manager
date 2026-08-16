package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/ratelimit"
	"appointment-manager/internal/session"
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	authLoginPath             = "/api/v1/auth/login"
	authLogoutPath            = "/api/v1/auth/logout"
	authContentTypeHeader     = "Content-Type"
	authSetCookieHeader       = "Set-Cookie"
	authJSONType              = "application/json"
	authProblemJSONType       = "application/problem+json"
	authCaseNilLogger         = "nil logger"
	authCaseNilSessionStore   = "nil session store"
	authCaseNilRepo           = "nil assistant repo"
	authCaseNilPasswordHasher = "nil password hasher"
	authCaseNilRateLimiter    = "nil rate limiter"

	roomyBurst               = 1000
	limiterRefill            = time.Minute
	limiterMaxEntries        = 128
	authCaseInvalidJSON      = "invalid json"
	authCaseMissingType      = "missing content type"
	authCaseUnknownField     = "unknown field"
	authCaseLogoutIdempotent = "logout idempotent"
)

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	repo := &assistant.PostgresRepository{}
	hasher := password.NewArgon2(nil)
	logger := slog.New(slog.DiscardHandler)
	limiter := newTestLimiter(t)

	tests := []struct {
		name     string
		logger   *slog.Logger
		store    *session.Store
		repo     *assistant.PostgresRepository
		hasher   *password.Argon2
		limiter  *ratelimit.Limiter
		expected error
	}{
		{name: authCaseNilLogger, logger: nil, store: store, repo: repo, hasher: hasher, limiter: limiter, expected: ErrNilLogger},
		{name: authCaseNilSessionStore, logger: logger, store: nil, repo: repo, hasher: hasher, limiter: limiter, expected: ErrNilSessionStore},
		{name: authCaseNilRepo, logger: logger, store: store, repo: nil, hasher: hasher, limiter: limiter, expected: ErrNilAssistantRepo},
		{name: authCaseNilPasswordHasher, logger: logger, store: store, repo: repo, hasher: nil, limiter: limiter, expected: ErrNilPasswordHasher},
		{name: authCaseNilRateLimiter, logger: logger, store: store, repo: repo, hasher: hasher, limiter: nil, expected: ErrNilRateLimiter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := NewHandler(tt.logger, tt.store, tt.repo, tt.hasher, tt.limiter, true)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestRegisterHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	h, err := NewHandler(
		slog.New(slog.DiscardHandler),
		newTestSessionStore(t),
		&assistant.PostgresRepository{},
		password.NewArgon2(nil),
		newTestLimiter(t),
		true,
	)
	require.NoError(t, err)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterHandlers(mux)
	})
}

func TestLoginEndpointDecodeFailures(t *testing.T) {
	t.Parallel()

	mux := newAuthDecodeTestMux(t)
	validBody := `{"email":"assistant@email.com","password":"123456"}`
	bodyWithUnknown := strings.TrimSuffix(validBody, "}") + `,"extra":"field"}`

	tests := []struct {
		name        string
		body        string
		contentType string
		expected    int
	}{
		{name: authCaseInvalidJSON, body: "{", contentType: authJSONType, expected: http.StatusBadRequest},
		{name: authCaseMissingType, body: validBody, contentType: "", expected: http.StatusUnsupportedMediaType},
		{name: authCaseUnknownField, body: bodyWithUnknown, contentType: authJSONType, expected: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, authLoginPath, bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set(authContentTypeHeader, tt.contentType)
			}
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.expected, rec.Code)
			assert.Equal(t, authProblemJSONType, rec.Header().Get(authContentTypeHeader))
		})
	}
}

func TestLogoutEndpointIdempotent(t *testing.T) {
	t.Parallel()

	mux := newAuthDecodeTestMux(t)

	tests := []struct {
		name       string
		withCookie bool
	}{
		{name: authCaseLogoutIdempotent + " without cookie", withCookie: false},
		{name: authCaseLogoutIdempotent + " with cookie", withCookie: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, authLogoutPath, nil)
			if tt.withCookie {
				//nolint:gosec // G124 false positive: this is a request cookie via AddCookie, which only serializes Name/Value; Secure/HttpOnly/SameSite are meaningless here.
				req.AddCookie(&http.Cookie{Name: session.CookieName, Value: "session-id"})
			}
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNoContent, rec.Code)
			setCookie := rec.Header().Get(authSetCookieHeader)
			require.NotEmpty(t, setCookie)
			assert.Contains(t, setCookie, session.CookieName+"=")
			assert.Contains(t, setCookie, "Max-Age=0")
		})
	}
}

// newTestLimiter builds a limiter roomy enough that no test trips it by
// accident. Tests that are about the limit ask for one sized to trip.
func newTestLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()

	return newLimiterWithBurst(t, roomyBurst)
}

func newLimiterWithBurst(t *testing.T, accountBurst int) *ratelimit.Limiter {
	t.Helper()

	limiter, err := ratelimit.New(ratelimit.Config{
		Enabled:       true,
		AccountBurst:  accountBurst,
		AccountRefill: limiterRefill,
		IPBurst:       roomyBurst,
		IPRefill:      limiterRefill,
		MaxEntries:    limiterMaxEntries,
	}, nil)
	require.NoError(t, err)

	return limiter
}

func newTestHandler(t *testing.T, hasher *password.Argon2) *Handler {
	t.Helper()

	return newTestHandlerWithLimiter(t, hasher, newTestLimiter(t))
}

func newTestHandlerWithLimiter(t *testing.T, hasher *password.Argon2, limiter *ratelimit.Limiter) *Handler {
	t.Helper()

	h, err := NewHandler(
		slog.New(slog.DiscardHandler),
		newTestSessionStore(t),
		&assistant.PostgresRepository{},
		hasher,
		limiter,
		true,
	)
	require.NoError(t, err)

	return h
}

func newAuthDecodeTestMux(t *testing.T) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()
	newTestHandler(t, password.NewArgon2(nil)).RegisterHandlers(mux)

	return mux
}
