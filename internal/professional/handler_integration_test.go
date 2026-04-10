//go:build integration

package professional_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	handlerLauraPayload          = `{"first_name":"Laura","last_name":"Gomez","phone":"1133334444"}`
	handlerAnaPayload            = `{"first_name":"Ana","last_name":"Perez","phone":"1144445555"}`
	handlerMartaPayload          = `{"first_name":"Marta","last_name":"Sosa","phone":"1166667777"}`
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

func TestListEndpointReturnsActiveProfessionals(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)
	mux := newProfessionalIntegrationMux(t, repo)

	createReqOne := newIntegrationCreateRequest(ctx, handlerLauraPayload)
	createRecOne := httptest.NewRecorder()
	mux.ServeHTTP(createRecOne, createReqOne)
	require.Equal(t, http.StatusCreated, createRecOne.Code)

	createReqTwo := newIntegrationCreateRequest(ctx, handlerAnaPayload)
	createRecTwo := httptest.NewRecorder()
	mux.ServeHTTP(createRecTwo, createReqTwo)
	require.Equal(t, http.StatusCreated, createRecTwo.Code)

	createReqThree := newIntegrationCreateRequest(ctx, handlerMartaPayload)
	createRecThree := httptest.NewRecorder()
	mux.ServeHTTP(createRecThree, createReqThree)
	require.Equal(t, http.StatusCreated, createRecThree.Code)

	inactiveID := professionalIDFromLocation(t, createRecTwo.Header().Get("Location"))
	setProfessionalActive(ctx, t, pool, inactiveID, false)

	listReq := httptest.NewRequestWithContext(ctx, http.MethodGet, handlerIntegrationPath, nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	assert.Equal(t, http.StatusOK, listRec.Code)
	assert.Equal(t, handlerIntegrationJSON, listRec.Header().Get(handlerIntegrationContentKey))

	list := decodeProfessionalsList(t, listRec)
	require.Len(t, list, 2)
	assert.Contains(t, []string{list[0].FirstName, list[1].FirstName}, "Laura")
	assert.Contains(t, []string{list[0].FirstName, list[1].FirstName}, "Marta")
	for i := range list {
		assert.True(t, list[i].Active)
	}
}

func TestListEndpointReturnsEmptyArrayWhenNoActiveProfessionals(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)
	mux := newProfessionalIntegrationMux(t, repo)

	createReq := newIntegrationCreateRequest(ctx, handlerLauraPayload)
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	inactiveID := professionalIDFromLocation(t, createRec.Header().Get("Location"))
	setProfessionalActive(ctx, t, pool, inactiveID, false)

	listReq := httptest.NewRequestWithContext(ctx, http.MethodGet, handlerIntegrationPath, nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	assert.Equal(t, http.StatusOK, listRec.Code)
	assert.Equal(t, handlerIntegrationJSON, listRec.Header().Get(handlerIntegrationContentKey))

	list := decodeProfessionalsList(t, listRec)
	assert.Empty(t, list)
}

func TestListEndpointReturnsInternalServerErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)
	mux := newProfessionalIntegrationMux(t, repo)

	pool.Close()

	listReq := httptest.NewRequestWithContext(ctx, http.MethodGet, handlerIntegrationPath, nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	assert.Equal(t, http.StatusInternalServerError, listRec.Code)
	assert.Equal(t, handlerIntegrationProblem, listRec.Header().Get(handlerIntegrationContentKey))
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

func decodeProfessionalsList(t *testing.T, rec *httptest.ResponseRecorder) []professionalPayload {
	t.Helper()

	var listed []professionalPayload
	err := json.Unmarshal(rec.Body.Bytes(), &listed)
	require.NoError(t, err)

	return listed
}

type professionalPayload struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Specialty string `json:"specialty"`
	Active    bool   `json:"active"`
}

func professionalIDFromLocation(t *testing.T, location string) string {
	t.Helper()

	require.True(t, strings.HasPrefix(location, handlerIntegrationPath+"/"))
	return strings.TrimPrefix(location, handlerIntegrationPath+"/")
}
