//go:build integration

package auth_test

import (
	"appointment-manager/internal/ratelimit"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	rateLimitHeaderLimit     = "X-RateLimit-Limit"
	rateLimitHeaderRemaining = "X-RateLimit-Remaining"
	rateLimitHeaderRetry     = "Retry-After"

	// Small enough to spend inside a test, large enough to leave room for the
	// success case to walk up to the edge and come back.
	rateLimitTestBurst = 3
)

func rateLimitTestConfig() ratelimit.Config {
	cfg := authRoomyLimiterConfig()
	cfg.AccountBurst = rateLimitTestBurst

	return cfg
}

func TestLoginSpendsItsAllowanceAndThenRefuses(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, newTestSessionStore(t), rateLimitTestConfig(), true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	for i := range rateLimitTestBurst {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyWrongPassword))

		require.Equalf(t, http.StatusUnauthorized, rec.Code, "attempt %d should reach the password check", i)
		assert.Equal(t, "3", rec.Header().Get(rateLimitHeaderLimit))
		assert.Equalf(t, strconv.Itoa(rateLimitTestBurst-i-1), rec.Header().Get(rateLimitHeaderRemaining), "attempt %d", i)
		assert.Emptyf(t, rec.Header().Get(rateLimitHeaderRetry), "attempt %d was allowed, so nothing to retry", i)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyWrongPassword))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, authContentTypeProblem, rec.Header().Get(authHeaderContentType))
	assert.Equal(t, "0", rec.Header().Get(rateLimitHeaderRemaining))
	assert.NotEmpty(t, rec.Header().Get(rateLimitHeaderRetry))
}

func TestSuccessfulLoginRestoresTheAccountAllowance(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, newTestSessionStore(t), rateLimitTestConfig(), true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	// Mistype every attempt but the last one available.
	for range rateLimitTestBurst - 1 {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyWrongPassword))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyValidLogin))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "3", rec.Header().Get(rateLimitHeaderRemaining),
		"getting it right must not leave the user rationed: there is no password reset to escape with")
}

func TestDisabledLimiterAdvertisesNothing(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)
	mux := newAuthIntegrationMux(t, repo, newTestSessionStore(t), authDisabledLimiterConfig(), true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyValidLogin))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(rateLimitHeaderLimit))
	assert.Empty(t, rec.Header().Get(rateLimitHeaderRemaining))
}

func TestLoginAllowanceIsPerAccount(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newAuthIntegrationPool(ctx, t)
	repo := newAuthIntegrationRepository(t, pool)

	// A roomy address budget so only the account limit can be the one that bites.
	cfg := rateLimitTestConfig()
	cfg.IPRefill = time.Hour
	mux := newAuthIntegrationMux(t, repo, newTestSessionStore(t), cfg, true)

	seedAssistantForAuth(ctx, t, repo, authEmail, authPassword)

	for range rateLimitTestBurst + 1 {
		mux.ServeHTTP(httptest.NewRecorder(), newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyWrongPassword))
	}

	// A different account from the same address still has its own budget: the
	// account that was hammered is the only one refused.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newAuthRequest(ctx, http.MethodPost, authPathLogin, authBodyUnknownEmail))

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"one account spending its allowance must not close another")
}
