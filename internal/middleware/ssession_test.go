package middleware_test

import (
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/session"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	middlewareSessionPath      = "/api/v1/protected"
	middlewareProblemJSON      = "application/problem+json"
	middlewareAuthRequiredText = "authentication required"
	middlewareInvalidSession   = "session is invalid or expired"
)

func TestSessionMiddlewareRejectsMissingCookie(t *testing.T) {
	t.Parallel()

	store := session.NewStore()
	handler := middleware.Session(store, false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not be called")
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, middlewareSessionPath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, middlewareProblemJSON, rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), middlewareAuthRequiredText)
}

func TestSessionMiddlewareRejectsInvalidCookieAndClearsIt(t *testing.T) {
	t.Parallel()

	store := session.NewStore()
	handler := middleware.Session(store, false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not be called")
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, middlewareSessionPath, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: "missing"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, middlewareProblemJSON, rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), middlewareInvalidSession)

	result := rec.Result()
	cookies := result.Cookies()
	require.NotEmpty(t, cookies)
	assert.Equal(t, session.CookieName, cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestSessionMiddlewareInjectsSessionInContext(t *testing.T) {
	t.Parallel()

	store := session.NewStore()
	sessionID, err := store.Create("assistant-1", "assistant@email.com")
	require.NoError(t, err)

	var capturedUserID string
	handler := middleware.Session(store, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, getErr := session.FromContext(r.Context())
		require.NoError(t, getErr)
		capturedUserID = s.UserID
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, middlewareSessionPath, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "assistant-1", capturedUserID)
}

func TestSessionMiddlewareHandlesNilStore(t *testing.T) {
	t.Parallel()

	handler := middleware.Session(nil, false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not be called")
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, middlewareSessionPath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, middlewareProblemJSON, rec.Header().Get("Content-Type"))
}
