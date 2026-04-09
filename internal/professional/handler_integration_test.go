//go:build integration

package professional_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	handlerIntegrationPath       = "/api/v1/professionals"
	handlerIntegrationContentKey = "Content-Type"
	handlerIntegrationJSON       = "application/json"
	handlerIntegrationProblem    = "application/problem+json"
	handlerIntegrationBody       = `{"first_name":"Laura","last_name":"Gomez","phone":"1133334444"}`
)

func TestCreateEndpointCreatesProfessionalAndReturnsLocation(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)
	mux := newProfessionalIntegrationMux(t, repo)

	req := newIntegrationCreateRequest(ctx, handlerIntegrationBody)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	location := rec.Header().Get("Location")
	require.NotEmpty(t, location)
	assert.Contains(t, location, handlerIntegrationPath+"/")

	total := countProfessionals(ctx, t, pool)
	assert.Equal(t, int64(1), total)
}

func TestCreateEndpointReturnsValidationForInvalidBody(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)
	mux := newProfessionalIntegrationMux(t, repo)

	req := newIntegrationCreateRequest(ctx, `{"first_name":"","last_name":"Gomez","phone":"1133334444"}`)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, handlerIntegrationProblem, rec.Header().Get(handlerIntegrationContentKey))
	assert.Equal(t, int64(0), countProfessionals(ctx, t, pool))
}

func TestCreateEndpointReturnsInternalServerErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)
	mux := newProfessionalIntegrationMux(t, repo)

	pool.Close()

	req := newIntegrationCreateRequest(ctx, handlerIntegrationBody)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, handlerIntegrationProblem, rec.Header().Get(handlerIntegrationContentKey))
}

func newIntegrationCreateRequest(ctx context.Context, body string) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, handlerIntegrationPath, bytes.NewBufferString(body))
	req.Header.Set(handlerIntegrationContentKey, handlerIntegrationJSON)

	return req
}

func countProfessionals(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var total int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM professional`).Scan(&total)
	require.NoError(t, err)

	return total
}
