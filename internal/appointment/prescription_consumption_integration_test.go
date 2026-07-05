//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	prescriptionStatusActiveValue    int16 = 1
	prescriptionStatusCompletedValue int16 = 2
)

// A patient without an active prescription cannot book: the booking is rejected
// before any slot check and no appointment is stored.
func TestCreateEndpointRejectsPatientWithoutActivePrescription(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	slotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	insertSlot(ctx, t, pool, slotID, professionalID, "2026-03-01", "09:00:00+00", "09:30:00+00", 2, false)

	mux := newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, slotID, patientID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
	assert.Equal(t, appointment.ErrNoActivePrescription.Error(), decodeProblemDetail(t, rec).Detail)

	assert.Equal(t, int64(0), countConfirmedAppointmentsForSlot(ctx, t, pool, slotID))
}

// Booking the last authorized session completes the prescription in the same
// transaction, which then prevents any further booking for that patient.
func TestCreateEndpointConsumesLastSessionAndCompletesPrescription(t *testing.T) {
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
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	prescriptionID := insertActivePrescription(ctx, t, pool, patientID, 1)
	insertSlot(ctx, t, pool, slotOneID, professionalID, "2026-03-02", "09:00:00+00", "09:30:00+00", 2, false)
	insertSlot(ctx, t, pool, slotTwoID, professionalID, "2026-03-02", "10:00:00+00", "10:30:00+00", 2, false)

	mux := newIntegrationMux(t, pool)

	firstRec := performCreateRequest(ctx, mux, createRequestBody(t, slotOneID, patientID, professionalID, assistantID, nil))
	require.Equal(t, http.StatusCreated, firstRec.Code)

	assert.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))

	secondRec := performCreateRequest(ctx, mux, createRequestBody(t, slotTwoID, patientID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusConflict, secondRec.Code)
	assert.Equal(t, appointment.ErrNoActivePrescription.Error(), decodeProblemDetail(t, secondRec).Detail)

	assert.Equal(t, int64(1), countConfirmedAppointmentsForSlot(ctx, t, pool, slotOneID))
	assert.Equal(t, int64(0), countConfirmedAppointmentsForSlot(ctx, t, pool, slotTwoID))
}

// When an active prescription has already consumed all of its sessions (here
// seeded directly to keep it ACTIVE), a new booking is rejected as exhausted.
func TestCreateEndpointRejectsWhenPrescriptionExhausted(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	consumedSlotID := uuid.Must(uuid.NewV7())
	newSlotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	prescriptionID := insertActivePrescription(ctx, t, pool, patientID, 1)
	insertSlot(ctx, t, pool, consumedSlotID, professionalID, "2026-03-03", "09:00:00+00", "09:30:00+00", 2, false)
	insertSlot(ctx, t, pool, newSlotID, professionalID, "2026-03-03", "10:00:00+00", "10:30:00+00", 2, false)

	// Direct insert consumes the single session without triggering the
	// auto-complete logic, leaving the prescription ACTIVE but exhausted.
	insertAppointment(ctx, t, pool, uuid.Must(uuid.NewV7()), consumedSlotID, patientID, professionalID, assistantID, statusConfirmedValue, nil)

	mux := newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, newSlotID, patientID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, appointment.ErrNoRemainingSessions.Error(), decodeProblemDetail(t, rec).Detail)

	assert.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
	assert.Equal(t, int64(0), countConfirmedAppointmentsForSlot(ctx, t, pool, newSlotID))
}

// The session balance view counts CONFIRMED (1), ABSENT (3) and ATTENDED (4)
// appointments as consumed, while CANCELLED (2) frees the session.
func TestPatientSessionBalanceViewExcludesCancelledAppointments(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())
	confirmedSlotID := uuid.Must(uuid.NewV7())
	cancelledSlotID := uuid.Must(uuid.NewV7())
	absentSlotID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	insertActivePrescription(ctx, t, pool, patientID, 3)
	insertSlot(ctx, t, pool, confirmedSlotID, professionalID, "2026-03-04", "09:00:00+00", "09:30:00+00", 2, false)
	insertSlot(ctx, t, pool, cancelledSlotID, professionalID, "2026-03-04", "10:00:00+00", "10:30:00+00", 2, false)
	insertSlot(ctx, t, pool, absentSlotID, professionalID, "2026-03-04", "11:00:00+00", "11:30:00+00", 2, false)

	insertAppointment(ctx, t, pool, uuid.Must(uuid.NewV7()), confirmedSlotID, patientID, professionalID, assistantID, statusConfirmedValue, nil)
	insertAppointment(ctx, t, pool, uuid.Must(uuid.NewV7()), cancelledSlotID, patientID, professionalID, assistantID, statusCancelledValue, nil)
	insertAppointment(ctx, t, pool, uuid.Must(uuid.NewV7()), absentSlotID, patientID, professionalID, assistantID, statusAbsentValue, nil)

	// total 3 - consumed 2 (confirmed + absent) = 1 remaining; cancelled excluded.
	assert.Equal(t, 1, fetchRemainingSessions(ctx, t, pool, patientID))
}

func fetchPrescriptionStatus(ctx context.Context, t *testing.T, pool *pgxpool.Pool, prescriptionID uuid.UUID) int16 {
	t.Helper()

	var status int16
	err := pool.QueryRow(ctx, `SELECT status FROM prescription WHERE id = $1`, prescriptionID).Scan(&status)
	require.NoError(t, err)

	return status
}

func fetchRemainingSessions(ctx context.Context, t *testing.T, pool *pgxpool.Pool, patientID uuid.UUID) int {
	t.Helper()

	var remaining int
	err := pool.QueryRow(ctx, `SELECT remaining_sessions FROM patient_session_balance WHERE patient_id = $1`, patientID).Scan(&remaining)
	require.NoError(t, err)

	return remaining
}
