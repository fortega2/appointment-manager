//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/session"
	"appointment-manager/internal/slot"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	uiAppointmentsPath   = "/appointments"
	uiNewAppointmentPath = "/appointments/new"
	contentTypeForm      = "application/x-www-form-urlencoded"
)

func newIntegrationUIMux(t *testing.T, pool *pgxpool.Pool) *http.ServeMux {
	t.Helper()

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	service, err := appointment.NewService(repo, nil)
	require.NoError(t, err)

	query, err := appointment.NewQuery(pool)
	require.NoError(t, err)

	prescriptionQuery, err := prescription.NewQuery(pool)
	require.NoError(t, err)

	profRepo, err := professional.NewRepository(pool)
	require.NoError(t, err)

	slotQuery, err := slot.NewQuery(pool)
	require.NoError(t, err)

	h, err := appointment.NewUIHandler(newIntegrationLogger(), service, query, prescriptionQuery, profRepo, slotQuery)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterUIHandlers(mux)

	return mux
}

func sessionContext(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, session.SessionKey, &session.Session{
		UserID: userID.String(),
		Email:  userID.String() + "@clinic.test",
	})
}

func performUICreateRequest(
	ctx context.Context,
	mux *http.ServeMux,
	assistantID, slotID, patientID, professionalID uuid.UUID,
) *httptest.ResponseRecorder {
	form := url.Values{
		"slot_id":         {slotID.String()},
		"patient_id":      {patientID.String()},
		"professional_id": {professionalID.String()},
	}
	reqCtx := sessionContext(ctx, assistantID)
	req := httptest.NewRequestWithContext(reqCtx, http.MethodPost, uiAppointmentsPath, bytes.NewBufferString(form.Encode()))
	req.Header.Set(contentTypeHeader, contentTypeForm)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	return rec
}

func fetchAppointmentAssistantID(ctx context.Context, t *testing.T, pool *pgxpool.Pool, slotID, patientID uuid.UUID) uuid.UUID {
	t.Helper()

	var assistantID uuid.UUID
	err := pool.QueryRow(ctx, `SELECT assistant_id FROM appointment WHERE slot_id = $1 AND patient_id = $2`, slotID, patientID).
		Scan(&assistantID)
	require.NoError(t, err)

	return assistantID
}

// TestShowDashboardUIHandlerFiltersToConfirmedAndNotYetEnded locks in the
// query.go fix: the dashboard must list only appointments that are both
// Confirmed and whose slot has not ended yet, excluding every other
// status/end_time combination.
func TestShowDashboardUIHandlerFiltersToConfirmedAndNotYetEnded(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC().Truncate(time.Second)

	futureConfirmedEnd := now.Add(3 * time.Hour)
	futureConfirmedID := seedAppointmentForAction(ctx, t, pool, now.Add(2*time.Hour), futureConfirmedEnd, statusConfirmedValue)

	futureCancelledEnd := now.Add(5 * time.Hour)
	futureCancelledID := seedAppointmentForAction(ctx, t, pool, now.Add(4*time.Hour), futureCancelledEnd, statusCancelledValue)

	pastConfirmedEnd := now.Add(-2 * time.Hour)
	pastConfirmedID := seedAppointmentForAction(ctx, t, pool, now.Add(-3*time.Hour), pastConfirmedEnd, statusConfirmedValue)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, uiAppointmentsPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, futureConfirmedEnd.Format(time.RFC3339), "not-yet-ended Confirmed appointment %s should be listed", futureConfirmedID)
	assert.NotContains(t, body, futureCancelledEnd.Format(time.RFC3339), "not-yet-ended Cancelled appointment %s should not be listed", futureCancelledID)
	assert.NotContains(t, body, pastConfirmedEnd.Format(time.RFC3339), "already-ended Confirmed appointment %s should not be listed", pastConfirmedID)
}

func TestShowCreateFormUIHandlerRendersAvailableOptions(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC().Truncate(time.Second)

	professionalID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertPatient(ctx, t, pool, patientID)
	date, startTime, endTime := slotValues(now.Add(time.Hour), now.Add(2*time.Hour))
	insertSlot(ctx, t, pool, slotID, professionalID, date, startTime, endTime, 1, false)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, uiNewAppointmentPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Dr. Laura Gomez")
	assert.Contains(t, body, "Pablo Sosa")
}

func TestCreateUIHandlerUsesSessionAssistantID(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC().Truncate(time.Second)

	professionalID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertPatient(ctx, t, pool, patientID)
	insertAssistant(ctx, t, pool, assistantID)
	date, startTime, endTime := slotValues(now.Add(time.Hour), now.Add(2*time.Hour))
	insertSlot(ctx, t, pool, slotID, professionalID, date, startTime, endTime, 1, false)

	mux := newIntegrationUIMux(t, pool)
	rec := performUICreateRequest(ctx, mux, assistantID, slotID, patientID, professionalID)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, assistantID, fetchAppointmentAssistantID(ctx, t, pool, slotID, patientID))
}

func TestCreateUIHandlerMissingSessionReturnsInternalServerError(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath, bytes.NewBufferString(url.Values{}.Encode()))
	req.Header.Set(contentTypeHeader, contentTypeForm)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCreateUIHandlerMalformedBodyReturnsBadRequest(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath, bytes.NewBufferString("slot_id=%zz"))
	req.Header.Set(contentTypeHeader, contentTypeForm)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateUIHandlerRejectsBlockedSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC().Truncate(time.Second)

	professionalID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertPatient(ctx, t, pool, patientID)
	insertAssistant(ctx, t, pool, assistantID)
	date, startTime, endTime := slotValues(now.Add(time.Hour), now.Add(2*time.Hour))
	insertSlot(ctx, t, pool, slotID, professionalID, date, startTime, endTime, 1, true)

	mux := newIntegrationUIMux(t, pool)
	rec := performUICreateRequest(ctx, mux, assistantID, slotID, patientID, professionalID)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestAttendUIAppointmentInvalidIDReturnsBadRequest(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	mux := newIntegrationUIMux(t, pool)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath+"/invalid/attend", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAttendUIAppointmentWithinRangeMarksAttended(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(ctx, t, pool, now.Add(-5*time.Minute), now.Add(25*time.Minute), statusConfirmedValue)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath+"/"+appointmentID.String()+"/attend", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusAttendedValue, status)
}

func TestAttendUIAppointmentOutsideRangeReturnsUnprocessableEntity(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(ctx, t, pool, now.Add(2*time.Hour), now.Add(3*time.Hour), statusConfirmedValue)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath+"/"+appointmentID.String()+"/attend", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusConfirmedValue, status)
}

func TestCancelUIAppointmentBefore24HoursMarksCancelled(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(ctx, t, pool, now.Add(30*time.Hour), now.Add(31*time.Hour), statusConfirmedValue)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath+"/"+appointmentID.String()+"/cancel", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusCancelledValue, status)
}

func TestCancelUIAppointmentInside24HoursMarksAbsent(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(ctx, t, pool, now.Add(2*time.Hour), now.Add(3*time.Hour), statusConfirmedValue)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath+"/"+appointmentID.String()+"/cancel", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusAbsentValue, status)
}

func TestCancelUIAppointmentInvalidStatusReturnsConflict(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()
	appointmentID := seedAppointmentForAction(ctx, t, pool, now.Add(-2*time.Hour), now.Add(-time.Hour), statusAttendedValue)

	mux := newIntegrationUIMux(t, pool)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, uiAppointmentsPath+"/"+appointmentID.String()+"/cancel", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusAttendedValue, status)
}
