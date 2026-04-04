//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/db"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	integrationImage    = "postgres:18-alpine"
	integrationDBName   = "appointment_manager"
	integrationDBUser   = "appointment_user"
	integrationDBPass   = "appointment_password"
	integrationSSLParam = "sslmode=disable"

	appointmentsEndpoint = "/api/v1/appointments"
	contentTypeHeader    = "Content-Type"
	contentTypeJSON      = "application/json"
	problemContentType   = "application/problem+json"
	writeFailureMessage  = "write failed"
)

func TestListEndpointFiltersAndPaginatesByStatus(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	fixture := seedAppointments(ctx, t, pool)

	h, err := appointment.NewHandler(newIntegrationLogger(), pool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	reqPage1 := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint+"?status=1&limit=1&page=1", nil)
	recPage1 := httptest.NewRecorder()
	mux.ServeHTTP(recPage1, reqPage1)

	assert.Equal(t, http.StatusOK, recPage1.Code)
	assert.Equal(t, contentTypeJSON, recPage1.Header().Get(contentTypeHeader))

	var page1 []appointment.Appointment
	err = json.Unmarshal(recPage1.Body.Bytes(), &page1)
	require.NoError(t, err)
	require.Len(t, page1, 1)
	assert.Equal(t, fixture.confirmedOldestID, page1[0].ID)

	reqPage2 := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint+"?status=1&limit=1&page=2", nil)
	recPage2 := httptest.NewRecorder()
	mux.ServeHTTP(recPage2, reqPage2)

	assert.Equal(t, http.StatusOK, recPage2.Code)

	var page2 []appointment.Appointment
	err = json.Unmarshal(recPage2.Body.Bytes(), &page2)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, fixture.confirmedNewestID, page2[0].ID)

	reqCancelled := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint+"?status=2&limit=10&page=1", nil)
	recCancelled := httptest.NewRecorder()
	mux.ServeHTTP(recCancelled, reqCancelled)

	assert.Equal(t, http.StatusOK, recCancelled.Code)

	var cancelled []appointment.Appointment
	err = json.Unmarshal(recCancelled.Body.Bytes(), &cancelled)
	require.NoError(t, err)
	require.Len(t, cancelled, 1)
	assert.Equal(t, fixture.cancelledID, cancelled[0].ID)
}

func TestListEndpointDefaultsToConfirmedStatus(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	fixture := seedAppointments(ctx, t, pool)

	h, err := appointment.NewHandler(newIntegrationLogger(), pool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var listed []appointment.Appointment
	err = json.Unmarshal(rec.Body.Bytes(), &listed)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, fixture.confirmedOldestID, listed[0].ID)
	assert.Equal(t, fixture.confirmedNewestID, listed[1].ID)
}

func TestListEndpointReturnsInternalServerErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	h, err := appointment.NewHandler(newIntegrationLogger(), pool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	pool.Close()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
}

func TestListEndpointReturnsInternalServerErrorWhenResponseWriteFails(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	h, err := appointment.NewHandler(newIntegrationLogger(), pool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint, nil)
	writer := newFailFirstWriteResponseWriter()
	mux.ServeHTTP(writer, req)

	assert.Equal(t, http.StatusInternalServerError, writer.statusCode)
	assert.Equal(t, problemContentType, writer.Header().Get(contentTypeHeader))
	assert.NotEmpty(t, writer.body.String())
}

type appointmentFixture struct {
	confirmedOldestID uuid.UUID
	confirmedNewestID uuid.UUID
	cancelledID       uuid.UUID
}

func seedAppointments(ctx context.Context, t *testing.T, pool *pgxpool.Pool) appointmentFixture {
	t.Helper()

	professionalID := uuid.New()
	assistantID := uuid.New()
	patientOneID := uuid.New()
	patientTwoID := uuid.New()
	patientThreeID := uuid.New()
	slotOneID := uuid.New()
	slotTwoID := uuid.New()
	slotThreeID := uuid.New()
	confirmedOldestID := uuid.New()
	confirmedNewestID := uuid.New()
	cancelledID := uuid.New()

	_, err := pool.Exec(ctx, `
		INSERT INTO professional (id, first_name, last_name, phone, specialty, active)
		VALUES ($1, 'Laura', 'Gomez', '1133334444', 'kinesiology', true)
	`, professionalID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO assistant (id, first_name, last_name, email, password_hash)
		VALUES ($1, 'Ana', 'Perez', 'ana.perez@clinic.test', 'hashed')
	`, assistantID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO patient (id, first_name, last_name, phone, email, health_insurance, insurance_number, clinical_notes)
		VALUES
			($1, 'Pablo', 'Sosa', '1111111111', 'pablo.sosa@clinic.test', 1, '00000000001', NULL),
			($2, 'Marta', 'Diaz', '2222222222', 'marta.diaz@clinic.test', 1, '00000000002', NULL),
			($3, 'Rocio', 'Lopez', '3333333333', 'rocio.lopez@clinic.test', 1, '00000000003', NULL)
	`, patientOneID, patientTwoID, patientThreeID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO slot (id, professional_id, date, start_time, end_time, max_capacity, blocked)
		VALUES
			($1, $4, '2026-01-01', '09:00:00+00', '09:30:00+00', 2, false),
			($2, $4, '2026-01-01', '10:00:00+00', '10:30:00+00', 2, false),
			($3, $4, '2026-01-01', '11:00:00+00', '11:30:00+00', 2, false)
	`, slotOneID, slotTwoID, slotThreeID, professionalID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO appointment (id, slot_id, patient_id, professional_id, assistant_id, status, notes, created_at)
		VALUES ($1, $2, $3, $4, $5, 1, NULL, '2026-01-10T08:00:00Z')
	`, confirmedOldestID, slotOneID, patientOneID, professionalID, assistantID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO appointment (id, slot_id, patient_id, professional_id, assistant_id, status, notes, created_at)
		VALUES ($1, $2, $3, $4, $5, 1, NULL, '2026-01-10T09:00:00Z')
	`, confirmedNewestID, slotTwoID, patientTwoID, professionalID, assistantID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO appointment (id, slot_id, patient_id, professional_id, assistant_id, status, notes, created_at)
		VALUES ($1, $2, $3, $4, $5, 2, NULL, '2026-01-10T10:00:00Z')
	`, cancelledID, slotThreeID, patientThreeID, professionalID, assistantID)
	require.NoError(t, err)

	return appointmentFixture{
		confirmedOldestID: confirmedOldestID,
		confirmedNewestID: confirmedNewestID,
		cancelledID:       cancelledID,
	}
}

func newIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		integrationImage,
		postgres.WithDatabase(integrationDBName),
		postgres.WithUsername(integrationDBUser),
		postgres.WithPassword(integrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, integrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newIntegrationLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type failFirstWriteResponseWriter struct {
	headers          http.Header
	body             bytes.Buffer
	statusCode       int
	failedFirstWrite bool
}

func newFailFirstWriteResponseWriter() *failFirstWriteResponseWriter {
	return &failFirstWriteResponseWriter{
		headers: http.Header{},
	}
}

func (w *failFirstWriteResponseWriter) Header() http.Header {
	return w.headers
}

func (w *failFirstWriteResponseWriter) Write(data []byte) (int, error) {
	if !w.failedFirstWrite {
		w.failedFirstWrite = true
		return 0, errors.New(writeFailureMessage)
	}

	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}

	return w.body.Write(data)
}

func (w *failFirstWriteResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}
