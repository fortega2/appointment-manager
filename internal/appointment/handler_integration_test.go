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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	integrationImage    = "postgres:18.3-alpine3.23"
	integrationDBName   = "appointment_manager"
	integrationDBUser   = "appointment_user"
	integrationDBPass   = "appointment_password"
	integrationSSLParam = "sslmode=disable"

	appointmentsEndpoint = "/api/v1/appointments"
	contentTypeHeader    = "Content-Type"
	contentTypeJSON      = "application/json"
	problemContentType   = "application/problem+json"
	writeFailureMessage  = "write failed"

	statusConfirmedValue int16 = 1
	statusCancelledValue int16 = 2
	statusAbsentValue    int16 = 3
	statusAttendedValue  int16 = 4
)

func TestListEndpointFiltersAndPaginatesByStatus(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	fixture := seedAppointments(ctx, t, pool)

	mux := newIntegrationMux(t, pool)

	reqPage1 := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint+"?status=1&limit=1&page=1", nil)
	recPage1 := httptest.NewRecorder()
	mux.ServeHTTP(recPage1, reqPage1)

	assert.Equal(t, http.StatusOK, recPage1.Code)
	assert.Equal(t, contentTypeJSON, recPage1.Header().Get(contentTypeHeader))

	var page1 []appointment.Appointment
	require.NoError(t, json.Unmarshal(recPage1.Body.Bytes(), &page1))
	require.Len(t, page1, 1)
	assert.Equal(t, fixture.confirmedOldestID, page1[0].ID)

	reqPage2 := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint+"?status=1&limit=1&page=2", nil)
	recPage2 := httptest.NewRecorder()
	mux.ServeHTTP(recPage2, reqPage2)

	assert.Equal(t, http.StatusOK, recPage2.Code)

	var page2 []appointment.Appointment
	require.NoError(t, json.Unmarshal(recPage2.Body.Bytes(), &page2))
	require.Len(t, page2, 1)
	assert.Equal(t, fixture.confirmedNewestID, page2[0].ID)

	reqCancelled := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint+"?status=2&limit=10&page=1", nil)
	recCancelled := httptest.NewRecorder()
	mux.ServeHTTP(recCancelled, reqCancelled)

	assert.Equal(t, http.StatusOK, recCancelled.Code)

	var cancelled []appointment.Appointment
	require.NoError(t, json.Unmarshal(recCancelled.Body.Bytes(), &cancelled))
	require.Len(t, cancelled, 1)
	assert.Equal(t, fixture.cancelledID, cancelled[0].ID)
}

func TestListEndpointDefaultsToConfirmedStatus(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	fixture := seedAppointments(ctx, t, pool)

	mux := newIntegrationMux(t, pool)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var listed []appointment.Appointment
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 2)
	assert.Equal(t, fixture.confirmedOldestID, listed[0].ID)
	assert.Equal(t, fixture.confirmedNewestID, listed[1].ID)
}

func TestListEndpointReturnsInternalServerErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	mux := newIntegrationMux(t, pool)

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

	mux := newIntegrationMux(t, pool)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, appointmentsEndpoint, nil)
	writer := newFailFirstWriteResponseWriter()
	mux.ServeHTTP(writer, req)

	assert.Equal(t, http.StatusInternalServerError, writer.statusCode)
	assert.Equal(t, problemContentType, writer.Header().Get(contentTypeHeader))
	assert.NotEmpty(t, writer.body.String())
}

func TestCreateEndpointStoresConfirmedStatusAndNullNotes(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotOneID := uuid.Must(uuid.NewV7())
	slotTwoID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)
	insertSlot(ctx, t, pool, slotOneID, professionalID, "2026-02-01", "09:00:00+00", "09:30:00+00", 2, false)
	insertSlot(ctx, t, pool, slotTwoID, professionalID, "2026-02-01", "10:00:00+00", "10:30:00+00", 2, false)

	mux := newIntegrationMux(t, pool)

	emptyNotes := ""
	firstRec := performCreateRequest(ctx, mux, createRequestBody(t, slotOneID, patientID, professionalID, assistantID, &emptyNotes))
	assert.Equal(t, http.StatusCreated, firstRec.Code)
	assert.Equal(t, contentTypeJSON, firstRec.Header().Get(contentTypeHeader))

	firstAppointmentID := appointmentIDFromLocation(t, firstRec.Header().Get("Location"))
	firstStatus, firstNotes := fetchAppointmentStatusAndNotes(ctx, t, pool, firstAppointmentID)
	assert.Equal(t, statusConfirmedValue, firstStatus)
	assert.Nil(t, firstNotes)

	secondRec := performCreateRequest(ctx, mux, createRequestBody(t, slotTwoID, patientID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusCreated, secondRec.Code)
	assert.Equal(t, contentTypeJSON, secondRec.Header().Get(contentTypeHeader))

	secondAppointmentID := appointmentIDFromLocation(t, secondRec.Header().Get("Location"))
	secondStatus, secondNotes := fetchAppointmentStatusAndNotes(ctx, t, pool, secondAppointmentID)
	assert.Equal(t, statusConfirmedValue, secondStatus)
	assert.Nil(t, secondNotes)
}

func TestCreateEndpointRejectsBlockedSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	blockedSlotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)
	insertSlot(ctx, t, pool, blockedSlotID, professionalID, "2026-02-02", "09:00:00+00", "09:30:00+00", 2, true)

	mux := newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, blockedSlotID, patientID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
	assert.Equal(t, appointment.ErrSlotBlocked.Error(), decodeProblemDetail(t, rec).Detail)

	assert.Equal(t, int64(0), countConfirmedAppointmentsForSlot(ctx, t, pool, blockedSlotID))
}

func TestCreateEndpointRejectsSlotWithoutAvailability(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientOneID := uuid.Must(uuid.NewV7())
	patientTwoID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientOneID)
	insertPatient(ctx, t, pool, patientTwoID)
	insertSlot(ctx, t, pool, slotID, professionalID, "2026-02-03", "10:00:00+00", "10:30:00+00", 1, false)
	insertAppointment(ctx, t, pool, uuid.Must(uuid.NewV7()), slotID, patientOneID, professionalID, assistantID, statusConfirmedValue, nil)

	mux := newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, slotID, patientTwoID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
	assert.Equal(t, appointment.ErrSlotWithoutAvailability.Error(), decodeProblemDetail(t, rec).Detail)

	assert.Equal(t, int64(1), countConfirmedAppointmentsForSlot(ctx, t, pool, slotID))
}

func TestCreateEndpointRejectsOverlappingAppointments(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalOneID := uuid.Must(uuid.NewV7())
	professionalTwoID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotOneID := uuid.Must(uuid.NewV7())
	slotTwoID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalOneID)
	insertProfessional(ctx, t, pool, professionalTwoID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)
	insertSlot(ctx, t, pool, slotOneID, professionalOneID, "2026-02-04", "12:00:00+00", "13:00:00+00", 2, false)
	insertSlot(ctx, t, pool, slotTwoID, professionalTwoID, "2026-02-04", "12:30:00+00", "13:30:00+00", 2, false)
	insertAppointment(ctx, t, pool, uuid.Must(uuid.NewV7()), slotOneID, patientID, professionalOneID, assistantID, statusConfirmedValue, nil)

	mux := newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, slotTwoID, patientID, professionalTwoID, assistantID, nil))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
	assert.Equal(t, appointment.ErrMultipleActiveAppointmentsDetected.Error(), decodeProblemDetail(t, rec).Detail)

	assert.Equal(t, int64(1), countConfirmedAppointmentsForPatient(ctx, t, pool, patientID))
}

func TestCreateEndpointReturnsUnprocessableEntityForInvalidReference(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)
	insertSlot(ctx, t, pool, slotID, professionalID, "2026-02-05", "15:00:00+00", "15:30:00+00", 2, false)

	mux := newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, slotID, patientID, uuid.Must(uuid.NewV7()), assistantID, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
	assert.Equal(t, appointment.ErrInvalidAppointmentReference.Error(), decodeProblemDetail(t, rec).Detail)

	assert.Equal(t, int64(0), countConfirmedAppointmentsForSlot(ctx, t, pool, slotID))
}

func TestCreateEndpointConcurrentRequestsRespectSlotCapacity(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientOneID := uuid.Must(uuid.NewV7())
	patientTwoID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientOneID)
	insertPatient(ctx, t, pool, patientTwoID)
	insertSlot(ctx, t, pool, slotID, professionalID, "2026-02-06", "10:00:00+00", "10:30:00+00", 1, false)

	mux := newIntegrationMux(t, pool)

	responses := performConcurrentCreateRequests(
		ctx,
		mux,
		createRequestBody(t, slotID, patientOneID, professionalID, assistantID, nil),
		createRequestBody(t, slotID, patientTwoID, professionalID, assistantID, nil),
	)

	statusCodes := statusCodesFromCreateResponses(responses)
	assert.Equal(t, []int{http.StatusCreated, http.StatusConflict}, statusCodes)
	assert.Equal(t, appointment.ErrSlotWithoutAvailability.Error(), conflictDetailFromCreateResponses(responses))
	assert.Equal(t, int64(1), countConfirmedAppointmentsForSlot(ctx, t, pool, slotID))
}

func TestCreateEndpointConcurrentRequestsPreventOverlappingAppointments(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalOneID := uuid.Must(uuid.NewV7())
	professionalTwoID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotOneID := uuid.Must(uuid.NewV7())
	slotTwoID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalOneID)
	insertProfessional(ctx, t, pool, professionalTwoID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)
	insertSlot(ctx, t, pool, slotOneID, professionalOneID, "2026-02-07", "12:00:00+00", "13:00:00+00", 2, false)
	insertSlot(ctx, t, pool, slotTwoID, professionalTwoID, "2026-02-07", "12:30:00+00", "13:30:00+00", 2, false)

	mux := newIntegrationMux(t, pool)

	responses := performConcurrentCreateRequests(
		ctx,
		mux,
		createRequestBody(t, slotOneID, patientID, professionalOneID, assistantID, nil),
		createRequestBody(t, slotTwoID, patientID, professionalTwoID, assistantID, nil),
	)

	statusCodes := statusCodesFromCreateResponses(responses)
	assert.Equal(t, []int{http.StatusCreated, http.StatusConflict}, statusCodes)
	assert.Equal(t, appointment.ErrMultipleActiveAppointmentsDetected.Error(), conflictDetailFromCreateResponses(responses))
	assert.Equal(t, int64(1), countConfirmedAppointmentsForPatient(ctx, t, pool, patientID))
}

func TestCancelEndpointBefore24HoursMarksCancelled(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(
		ctx,
		t,
		pool,
		now.Add(30*time.Hour),
		now.Add(31*time.Hour),
		statusConfirmedValue,
	)

	mux := newIntegrationMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint+"/"+appointmentID.String()+"/cancel", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusCancelledValue, status)
}

func TestCancelEndpointInside24HoursMarksAbsent(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(
		ctx,
		t,
		pool,
		now.Add(2*time.Hour),
		now.Add(3*time.Hour),
		statusConfirmedValue,
	)

	mux := newIntegrationMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint+"/"+appointmentID.String()+"/cancel", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusAbsentValue, status)
}

func TestAttendEndpointWithinRangeMarksAttended(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(
		ctx,
		t,
		pool,
		now.Add(-5*time.Minute),
		now.Add(25*time.Minute),
		statusConfirmedValue,
	)

	mux := newIntegrationMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint+"/"+appointmentID.String()+"/attend", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusAttendedValue, status)
}

func TestAttendEndpointOutsideRangeReturnsUnprocessableEntity(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(
		ctx,
		t,
		pool,
		now.Add(2*time.Hour),
		now.Add(3*time.Hour),
		statusConfirmedValue,
	)

	mux := newIntegrationMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint+"/"+appointmentID.String()+"/attend", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
	assert.Equal(t, appointment.ErrAppointmentCannotAttendNow.Error(), decodeProblemDetail(t, rec).Detail)

	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusConfirmedValue, status)
}

type appointmentFixture struct {
	confirmedOldestID uuid.UUID
	confirmedNewestID uuid.UUID
	cancelledID       uuid.UUID
}

type createAppointmentRequest struct {
	SlotID         string  `json:"slot_id"`
	PatientID      string  `json:"patient_id"`
	ProfessionalID string  `json:"professional_id"`
	AssistantID    string  `json:"assistant_id"`
	Notes          *string `json:"notes,omitempty"`
}

type problemResponse struct {
	Detail string `json:"detail"`
}

type createResponse struct {
	StatusCode int
	Detail     string
}

func seedAppointments(ctx context.Context, t *testing.T, pool *pgxpool.Pool) appointmentFixture {
	t.Helper()

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientOneID := uuid.Must(uuid.NewV7())
	patientTwoID := uuid.Must(uuid.NewV7())
	patientThreeID := uuid.Must(uuid.NewV7())
	slotOneID := uuid.Must(uuid.NewV7())
	slotTwoID := uuid.Must(uuid.NewV7())
	slotThreeID := uuid.Must(uuid.NewV7())
	confirmedOldestID := uuid.Must(uuid.NewV7())
	confirmedNewestID := uuid.Must(uuid.NewV7())
	cancelledID := uuid.Must(uuid.NewV7())

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
			($1, $4, '2026-01-01', '2026-01-01T09:00:00Z', '2026-01-01T09:30:00Z', 2, false),
			($2, $4, '2026-01-01', '2026-01-01T10:00:00Z', '2026-01-01T10:30:00Z', 2, false),
			($3, $4, '2026-01-01', '2026-01-01T11:00:00Z', '2026-01-01T11:30:00Z', 2, false)
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

func newIntegrationMux(t *testing.T, pool *pgxpool.Pool) *http.ServeMux {
	t.Helper()

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	service, err := appointment.NewService(repo)
	require.NoError(t, err)

	h, err := appointment.NewHandler(newIntegrationLogger(), service)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func seedAppointmentForAction(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	start time.Time,
	end time.Time,
	status int16,
) uuid.UUID {
	t.Helper()

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())
	appointmentID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)

	date, startTime, endTime := slotValues(start.UTC(), end.UTC())
	insertSlot(ctx, t, pool, slotID, professionalID, date, startTime, endTime, 1, false)
	insertAppointment(ctx, t, pool, appointmentID, slotID, patientID, professionalID, assistantID, status, nil)

	return appointmentID
}

func slotValues(start, end time.Time) (string, string, string) {
	return start.Format("2006-01-02"), start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func createRequestBody(
	t *testing.T,
	slotID uuid.UUID,
	patientID uuid.UUID,
	professionalID uuid.UUID,
	assistantID uuid.UUID,
	notes *string,
) []byte {
	t.Helper()

	body, err := json.Marshal(createAppointmentRequest{
		SlotID:         slotID.String(),
		PatientID:      patientID.String(),
		ProfessionalID: professionalID.String(),
		AssistantID:    assistantID.String(),
		Notes:          notes,
	})
	require.NoError(t, err)

	return body
}

func performCreateRequest(ctx context.Context, mux *http.ServeMux, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint, bytes.NewReader(body))
	req.Header.Set(contentTypeHeader, contentTypeJSON)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func appointmentIDFromLocation(t *testing.T, location string) uuid.UUID {
	t.Helper()

	require.True(t, strings.HasPrefix(location, appointmentsEndpoint+"/"))
	id, err := uuid.Parse(strings.TrimPrefix(location, appointmentsEndpoint+"/"))
	require.NoError(t, err)

	return id
}

func fetchAppointmentStatusAndNotes(ctx context.Context, t *testing.T, pool *pgxpool.Pool, appointmentID uuid.UUID) (int16, *string) {
	t.Helper()

	var status int16
	var notes *string
	err := pool.QueryRow(ctx, `
		SELECT
			status,
			notes
		FROM
			appointment
		WHERE
			id = $1
	`, appointmentID).Scan(&status, &notes)
	require.NoError(t, err)

	return status, notes
}

func decodeProblemDetail(t *testing.T, rec *httptest.ResponseRecorder) problemResponse {
	t.Helper()

	var problem problemResponse
	err := json.Unmarshal(rec.Body.Bytes(), &problem)
	require.NoError(t, err)

	return problem
}

func decodeProblemDetailFromBody(body []byte) problemResponse {
	var problem problemResponse
	if err := json.Unmarshal(body, &problem); err != nil {
		return problemResponse{}
	}

	return problem
}

func performConcurrentCreateRequests(ctx context.Context, mux *http.ServeMux, bodies ...[]byte) []createResponse {
	responses := make([]createResponse, len(bodies))
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := range bodies {
		i := i
		wg.Go(func() {
			<-start

			rec := performCreateRequest(ctx, mux, bodies[i])
			responses[i] = createResponse{StatusCode: rec.Code}
			if rec.Code >= http.StatusBadRequest {
				responses[i].Detail = decodeProblemDetailFromBody(rec.Body.Bytes()).Detail
			}
		})
	}

	close(start)
	wg.Wait()

	return responses
}

func statusCodesFromCreateResponses(responses []createResponse) []int {
	statusCodes := make([]int, 0, len(responses))
	for i := range responses {
		statusCodes = append(statusCodes, responses[i].StatusCode)
	}

	slices.Sort(statusCodes)

	return statusCodes
}

func conflictDetailFromCreateResponses(responses []createResponse) string {
	for i := range responses {
		if responses[i].StatusCode == http.StatusConflict {
			return responses[i].Detail
		}
	}

	return ""
}

func countConfirmedAppointmentsForSlot(ctx context.Context, t *testing.T, pool *pgxpool.Pool, slotID uuid.UUID) int64 {
	t.Helper()

	var total int64
	err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*)
		FROM
			appointment
		WHERE
			slot_id = $1
			AND status = $2
	`, slotID, statusConfirmedValue).Scan(&total)
	require.NoError(t, err)

	return total
}

func countConfirmedAppointmentsForPatient(ctx context.Context, t *testing.T, pool *pgxpool.Pool, patientID uuid.UUID) int64 {
	t.Helper()

	var total int64
	err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*)
		FROM
			appointment
		WHERE
			patient_id = $1
			AND status = $2
	`, patientID, statusConfirmedValue).Scan(&total)
	require.NoError(t, err)

	return total
}

func insertProfessional(ctx context.Context, t *testing.T, pool *pgxpool.Pool, professionalID uuid.UUID) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO professional (id, first_name, last_name, phone, specialty, active)
		VALUES ($1, 'Laura', 'Gomez', '1133334444', 'kinesiology', true)
	`, professionalID)
	require.NoError(t, err)
}

func insertAssistant(ctx context.Context, t *testing.T, pool *pgxpool.Pool, assistantID uuid.UUID) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO assistant (id, first_name, last_name, email, password_hash)
		VALUES ($1, 'Ana', 'Perez', $2, 'hashed')
	`, assistantID, assistantID.String()+"@clinic.test")
	require.NoError(t, err)
}

func insertPatient(ctx context.Context, t *testing.T, pool *pgxpool.Pool, patientID uuid.UUID) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO patient (id, first_name, last_name, phone, email, health_insurance, insurance_number, clinical_notes)
		VALUES ($1, 'Pablo', 'Sosa', '1111111111', $2, 1, $3, NULL)
	`, patientID, patientID.String()+"@clinic.test", patientID.String()[:11])
	require.NoError(t, err)
}

func insertSlot(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	slotID uuid.UUID,
	professionalID uuid.UUID,
	date string,
	startTime string,
	endTime string,
	maxCapacity int16,
	blocked bool,
) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO slot (id, professional_id, date, start_time, end_time, max_capacity, blocked)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, slotID, professionalID, date, startTime, endTime, maxCapacity, blocked)
	require.NoError(t, err)
}

func insertAppointment(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	appointmentID uuid.UUID,
	slotID uuid.UUID,
	patientID uuid.UUID,
	professionalID uuid.UUID,
	assistantID uuid.UUID,
	status int16,
	notes *string,
) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO appointment (id, slot_id, patient_id, professional_id, assistant_id, status, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, appointmentID, slotID, patientID, professionalID, assistantID, status, notes)
	require.NoError(t, err)
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
