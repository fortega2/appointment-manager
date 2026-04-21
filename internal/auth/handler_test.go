package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	authCaseInvalidJSON       = "invalid json"
	authCaseMissingType       = "missing content type"
	authCaseUnknownField      = "unknown field"
	authCaseLogoutIdempotent  = "logout idempotent"
)

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	store := session.NewStore()
	repo := &assistant.PostgresRepository{}
	hasher := password.NewArgon2()
	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name     string
		logger   *slog.Logger
		store    *session.Store
		repo     *assistant.PostgresRepository
		hasher   *password.Argon2
		expected error
	}{
		{name: authCaseNilLogger, logger: nil, store: store, repo: repo, hasher: hasher, expected: ErrNilLogger},
		{name: authCaseNilSessionStore, logger: logger, store: nil, repo: repo, hasher: hasher, expected: ErrNilSessionStore},
		{name: authCaseNilRepo, logger: logger, store: store, repo: nil, hasher: hasher, expected: ErrNilAssistantRepo},
		{name: authCaseNilPasswordHasher, logger: logger, store: store, repo: repo, hasher: nil, expected: ErrNilPasswordHasher},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := NewHandler(tt.logger, tt.store, tt.repo, tt.hasher, true)

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
		session.NewStore(),
		&assistant.PostgresRepository{},
		password.NewArgon2(),
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

func newAuthDecodeTestMux(t *testing.T) *http.ServeMux {
	t.Helper()

	h, err := NewHandler(
		slog.New(slog.DiscardHandler),
		session.NewStore(),
		&assistant.PostgresRepository{},
		password.NewArgon2(),
		true,
	)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}
