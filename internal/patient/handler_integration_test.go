//go:build integration

package patient_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
)

const (
	handlerIntegrationPath             = "/api/v1/patients"
	handlerIntegrationContentType      = "Content-Type"
	handlerIntegrationJSONType         = "application/json"
	handlerIntegrationProblemJSONType  = "application/problem+json"
	handlerIntegrationValidBody        = `{"first_name":"Laura","last_name":"Gomez","phone":"1133334444","email":"laura@mail.com","health_insurance":1,"insurance_number":"12345678901"}`
	handlerIntegrationInvalidInsBody   = `{"first_name":"Laura","last_name":"Gomez","phone":"1133334444","email":"laura@mail.com","health_insurance":999,"insurance_number":"12345678901"}`
	handlerIntegrationInvalidFieldBody = `{"first_name":"","last_name":"Gomez","phone":"1133334444","email":"laura@mail.com","health_insurance":1,"insurance_number":"12345678901"}`
)

func TestCreateEndpointCreatesPatient(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)
	mux := newPatientIntegrationMux(t, repo, pool)

	req := newIntegrationCreateRequest(ctx, handlerIntegrationValidBody)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, int64(1), countPatients(ctx, t, pool))
}

func TestCreateEndpointReturnsValidationForInvalidHealthInsurance(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)
	mux := newPatientIntegrationMux(t, repo, pool)

	req := newIntegrationCreateRequest(ctx, handlerIntegrationInvalidInsBody)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, handlerIntegrationProblemJSONType, rec.Header().Get(handlerIntegrationContentType))
	assert.Equal(t, int64(0), countPatients(ctx, t, pool))
}

func TestCreateEndpointReturnsValidationForInvalidBody(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)
	mux := newPatientIntegrationMux(t, repo, pool)

	req := newIntegrationCreateRequest(ctx, handlerIntegrationInvalidFieldBody)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, handlerIntegrationProblemJSONType, rec.Header().Get(handlerIntegrationContentType))
	assert.Equal(t, int64(0), countPatients(ctx, t, pool))
}

func TestCreateEndpointReturnsInternalServerErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)
	mux := newPatientIntegrationMux(t, repo, pool)

	pool.Close()

	req := newIntegrationCreateRequest(ctx, handlerIntegrationValidBody)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, handlerIntegrationProblemJSONType, rec.Header().Get(handlerIntegrationContentType))
}

func newIntegrationCreateRequest(ctx context.Context, body string) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, handlerIntegrationPath, bytes.NewBufferString(body))
	req.Header.Set(handlerIntegrationContentType, handlerIntegrationJSONType)

	return req
}
