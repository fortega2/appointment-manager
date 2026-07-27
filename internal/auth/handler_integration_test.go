//go:build integration

package auth_test

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	authPathLogin                = "/api/v1/auth/login"
	authPathLogout               = "/api/v1/auth/logout"
	authHeaderContentType        = "Content-Type"
	authHeaderSetCookie          = "Set-Cookie"
	authContentTypeJSON          = "application/json"
	authContentTypeProblem       = "application/problem+json"
	authBodyValidLogin           = `{"email":"assistant@email.com","password":"123456"}`
	authBodyWrongPassword        = `{"email":"assistant@email.com","password":"wrong"}` //nolint:gosec // G101 false positive: test fixture for a wrong-password case, not a real credential
	authBodyUnknownEmail         = `{"email":"unknown@email.com","password":"123456"}`
	authEmail                    = "assistant@email.com"
	authPassword                 = "123456"
	authCookieSecureDirective    = "Secure"
	authCookieHTTPOnlyDirective  = "HttpOnly"
	authCookieSameSiteStrictPart = "SameSite=Strict"
	authPathUILogin              = "/login"
	authHeaderHXRedirect         = "HX-Redirect"
	authFormContentType          = "application/x-www-form-urlencoded"
)

func TestLoginEndpointSuccessSetsCookieAndCreatesSession(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	store := session.NewStore()
	mux := newAuthIntegrationMux(t, repo, store, true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	req := newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyValidLogin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	setCookie := rec.Header().Get(authHeaderSetCookie)
	require.NotEmpty(t, setCookie)
	assert.Contains(t, setCookie, session.CookieName+"=")
	assert.Contains(t, setCookie, authCookieHTTPOnlyDirective)
	assert.Contains(t, setCookie, authCookieSameSiteStrictPart)
	assert.NotContains(t, setCookie, authCookieSecureDirective)

	cookie := extractSessionCookie(t, rec)
	sessionValue, err := store.Get(cookie.Value)
	require.NoError(t, err)
	require.NotNil(t, sessionValue)
	assert.Equal(t, authEmail, sessionValue.Email)
}

func TestLoginEndpointUnauthorizedForWrongPassword(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, session.NewStore(), true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	req := newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyWrongPassword)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, authContentTypeProblem, rec.Header().Get(authHeaderContentType))
	assert.Empty(t, rec.Header().Get(authHeaderSetCookie))
}

func TestLoginEndpointUnauthorizedForUnknownEmail(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, session.NewStore(), true)

	req := newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyUnknownEmail)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, authContentTypeProblem, rec.Header().Get(authHeaderContentType))
}

func TestLogoutEndpointIsIdempotent(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	store := session.NewStore()
	mux := newAuthIntegrationMux(t, repo, store, true)

	sessionID, err := store.Create("assistant-1", authEmail)
	require.NoError(t, err)

	withCookieReq := httptest.NewRequestWithContext(ctx, http.MethodPost, authPathLogout, nil)
	//nolint:gosec // G124 false positive: this is a request cookie via AddCookie, which only serializes Name/Value; Secure/HttpOnly/SameSite are meaningless here.
	withCookieReq.AddCookie(&http.Cookie{Name: session.CookieName, Value: sessionID})
	withCookieRec := httptest.NewRecorder()
	mux.ServeHTTP(withCookieRec, withCookieReq)

	assert.Equal(t, http.StatusNoContent, withCookieRec.Code)
	require.NotEmpty(t, withCookieRec.Header().Get(authHeaderSetCookie))

	_, getErr := store.Get(sessionID)
	require.Error(t, getErr)

	withoutCookieReq := httptest.NewRequestWithContext(ctx, http.MethodPost, authPathLogout, nil)
	withoutCookieRec := httptest.NewRecorder()
	mux.ServeHTTP(withoutCookieRec, withoutCookieReq)

	assert.Equal(t, http.StatusNoContent, withoutCookieRec.Code)
	require.NotEmpty(t, withoutCookieRec.Header().Get(authHeaderSetCookie))
}

func TestLoginEndpointSetsSecureCookieOutsideDevelopment(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, session.NewStore(), false)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	req := newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyValidLogin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get(authHeaderSetCookie), authCookieSecureDirective)
}

func TestLoginEndpointRejectsWhenTooManyConcurrentPasswordChecks(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, session.NewStore(), true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	const concurrentLogins = 20 // well above the shared Argon2 concurrent-hash cap

	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, concurrentLogins)

	for i := range concurrentLogins {
		wg.Go(func() {
			<-start

			req := newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyValidLogin)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			codes[i] = rec.Code
		})
	}
	close(start)
	wg.Wait()

	var busyCount int
	for _, code := range codes {
		assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, code)
		if code == http.StatusServiceUnavailable {
			busyCount++
		}
	}

	assert.Positive(t, busyCount, "expected at least one request to be rejected as busy under concurrent load")
}

func TestProcessLoginUIHandlerSuccessSetsCookieAndRedirects(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	store := session.NewStore()
	mux := newAuthIntegrationMux(t, repo, store, true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	req := newAuthFormRequest(ctx, authEmail, authPassword)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/", rec.Header().Get(authHeaderHXRedirect))

	setCookie := rec.Header().Get(authHeaderSetCookie)
	require.NotEmpty(t, setCookie)
	assert.Contains(t, setCookie, session.CookieName+"=")

	cookie := extractSessionCookie(t, rec)
	sessionValue, err := store.Get(cookie.Value)
	require.NoError(t, err)
	assert.Equal(t, authEmail, sessionValue.Email)
}

func TestProcessLoginUIHandlerRendersErrorForWrongPassword(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, session.NewStore(), true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	req := newAuthFormRequest(ctx, authEmail, "wrong-password")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(authHeaderHXRedirect))
	assert.Empty(t, rec.Header().Get(authHeaderSetCookie))
	assert.Contains(t, rec.Body.String(), "Incorrect email or password")
}

func newAuthFormRequest(ctx context.Context, email, plainPassword string) *http.Request {
	form := url.Values{"email": {email}, "password": {plainPassword}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, authPathUILogin, strings.NewReader(form.Encode()))
	req.Header.Set(authHeaderContentType, authFormContentType)

	return req
}

func newAuthRequest(ctx context.Context, method, path, body string) *http.Request {
	req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewBufferString(body))
	req.Header.Set(authHeaderContentType, authContentTypeJSON)

	return req
}

func extractSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == session.CookieName {
			return cookie
		}
	}

	t.Fatal("session cookie not found")
	return nil
}

func seedAssistantForAuth(
	ctx context.Context,
	t *testing.T,
	repo *assistant.PostgresRepository,
	email string,
	plainPassword string,
) {
	t.Helper()

	hasher := password.NewArgon2()
	hashedPassword, err := hasher.Hash(plainPassword)
	require.NoError(t, err)

	record, err := assistant.NewAssistant("Laura", "Gomez", email, hashedPassword)
	require.NoError(t, err)

	_, err = repo.Create(ctx, *record)
	require.NoError(t, err)
}
