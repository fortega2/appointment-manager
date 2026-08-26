//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const reopenSlotCapacity int16 = 2

// Booking the last authorized session completes the prescription, which is a
// cached way of saying "every session is used up". Cancelling that booking gives
// the session back and makes the cache wrong, so the prescription has to become
// ACTIVE again -- otherwise the freed session is unreachable: the patient
// vanishes from patient_session_balance (and so from the booking form) and a
// direct attempt is rejected as having no active prescription.
func TestCancelFreeingLastSessionReopensCompletedPrescription(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	patientID := uuid.NewV7()
	firstSlotID := uuid.NewV7()
	secondSlotID := uuid.NewV7()

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	prescriptionID := insertActivePrescription(ctx, t, pool, patientID, 1)
	insertReopenSlot(ctx, t, pool, firstSlotID, professionalID, now.Add(30*time.Hour))
	insertReopenSlot(ctx, t, pool, secondSlotID, professionalID, now.Add(40*time.Hour))

	mux := newIntegrationMux(t, pool)

	bookRec := performCreateRequest(ctx, mux, createRequestBody(t, firstSlotID, patientID, professionalID, assistantID, nil))
	require.Equal(t, http.StatusCreated, bookRec.Code)
	appointmentID := appointmentIDFromLocation(t, bookRec.Header().Get("Location"))
	require.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))

	// More than 24h out, so this is a real cancellation (status 2) rather than a
	// no-show, and the session genuinely goes back to the patient.
	require.Equal(t, http.StatusNoContent, performCancelRequest(ctx, mux, appointmentID).Code)
	cancelledStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	require.Equal(t, statusCancelledValue, cancelledStatus)

	assert.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
	assert.Equal(t, 1, fetchRemainingSessions(ctx, t, pool, patientID))

	rebookRec := performCreateRequest(ctx, mux, createRequestBody(t, secondSlotID, patientID, professionalID, assistantID, nil))
	assert.Equal(t, http.StatusCreated, rebookRec.Code, "the freed session must be bookable again")
}

// Cancelling inside the 24h window marks the patient ABSENT, which still
// consumes the session. The prescription is therefore genuinely still exhausted
// and must stay COMPLETED. This is the case that keeps the reopen honest: it
// passes only because the decision is recomputed from the appointment rows, so
// anyone replacing that count with "this path cancelled something, reopen it"
// breaks here.
func TestCancelInside24HoursKeepsPrescriptionCompleted(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	patientID, prescriptionID, appointmentID, mux := seedCompletedPrescriptionBooking(ctx, t, pool, time.Now().UTC().Add(2*time.Hour))

	require.Equal(t, http.StatusNoContent, performCancelRequest(ctx, mux, appointmentID).Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	require.Equal(t, statusAbsentValue, status)

	assert.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
	assert.Zero(t, countActivePrescriptions(ctx, t, pool, patientID))
}

func TestAttendLastSessionKeepsPrescriptionCompleted(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	// The slot has to be open right now for attendance to be allowed.
	_, prescriptionID, appointmentID, mux := seedCompletedPrescriptionBooking(ctx, t, pool, time.Now().UTC().Add(-5*time.Minute))

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint+"/"+appointmentID.String()+"/attend", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	assert.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
}

// Once the patient has been issued a newer prescription, the old one stays
// COMPLETED and the freed session is deliberately forfeited: reopening it would
// leave the patient with two ACTIVE prescriptions, which idx_prescription_active
// _per_patient forbids, and failing the cancellation over an unrelated admin
// action is worse than losing the session. What must never happen is the
// cancellation itself being refused.
func TestCancelDoesNotReopenWhenNewerPrescriptionIsActive(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	patientID, oldPrescriptionID, appointmentID, mux := seedCompletedPrescriptionBooking(ctx, t, pool, time.Now().UTC().Add(30*time.Hour))
	newPrescriptionID := insertActivePrescription(ctx, t, pool, patientID, 5)

	require.Equal(t, http.StatusNoContent, performCancelRequest(ctx, mux, appointmentID).Code)
	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	require.Equal(t, statusCancelledValue, status, "the cancellation must succeed regardless")

	assert.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, oldPrescriptionID))
	assert.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, newPrescriptionID))
	assert.Equal(t, 1, countActivePrescriptions(ctx, t, pool, patientID))
}

// A second cancellation finds the prescription already reopened. The COMPLETED
// guard makes that a no-op instead of an error, and each freed session still
// shows up in the balance.
func TestCancellingASecondSessionLeavesTheReopenedPrescriptionAlone(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	patientID := uuid.NewV7()

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	prescriptionID := insertActivePrescription(ctx, t, pool, patientID, 3)

	mux := newIntegrationMux(t, pool)

	appointmentIDs := make([]uuid.UUID, 0, 3)
	for i := range 3 {
		slotID := uuid.NewV7()
		insertReopenSlot(ctx, t, pool, slotID, professionalID, now.Add(time.Duration(30+i)*time.Hour))

		rec := performCreateRequest(ctx, mux, createRequestBody(t, slotID, patientID, professionalID, assistantID, nil))
		require.Equal(t, http.StatusCreated, rec.Code)
		appointmentIDs = append(appointmentIDs, appointmentIDFromLocation(t, rec.Header().Get("Location")))
	}
	require.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))

	require.Equal(t, http.StatusNoContent, performCancelRequest(ctx, mux, appointmentIDs[0]).Code)
	require.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
	require.Equal(t, 1, fetchRemainingSessions(ctx, t, pool, patientID))

	require.Equal(t, http.StatusNoContent, performCancelRequest(ctx, mux, appointmentIDs[1]).Code)
	assert.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
	assert.Equal(t, 2, fetchRemainingSessions(ctx, t, pool, patientID))
}

// A clinic-side cancellation frees the session just as a patient-side one does,
// so withdrawing the slot that held the final booking must reopen the
// prescription too.
func TestCancelBySlotReopensCompletedPrescription(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	patientID := uuid.NewV7()
	slotID := uuid.NewV7()

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	prescriptionID := insertActivePrescription(ctx, t, pool, patientID, 1)
	insertReopenSlot(ctx, t, pool, slotID, professionalID, now.Add(30*time.Hour))

	mux := newIntegrationMux(t, pool)
	require.Equal(t, http.StatusCreated, performCreateRequest(ctx, mux, createRequestBody(t, slotID, patientID, professionalID, assistantID, nil)).Code)
	require.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.CancelBySlot(ctx, slotID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	assert.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))
	assert.Equal(t, 1, fetchRemainingSessions(ctx, t, pool, patientID))
}

// The reconciliation sweep spans many slots at once, so a single patient can
// have two completed prescriptions freed by the same run. Only one of them may
// become ACTIVE — idx_prescription_active_per_patient allows no more — and the
// sweep must not fail over it, because a failed sweep leaves the appointments
// stranded as CONFIRMED on slots the clinic already withdrew.
//
// This is what forces the reopen to be one statement per prescription. Reopening
// them in a single set-based update evaluates both against the same snapshot,
// sets both ACTIVE and loses the entire sweep to a unique violation.
func TestCancelOnBlockedSlotsReopensAtMostOnePrescriptionPerPatient(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	patientID := uuid.NewV7()
	firstSlotID := uuid.NewV7()
	secondSlotID := uuid.NewV7()

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	insertReopenSlot(ctx, t, pool, firstSlotID, professionalID, now.Add(30*time.Hour))
	insertReopenSlot(ctx, t, pool, secondSlotID, professionalID, now.Add(40*time.Hour))

	mux := newIntegrationMux(t, pool)

	// Completing the first prescription frees the partial unique index, which is
	// exactly what lets a second one be issued and then completed as well.
	firstPrescriptionID := insertActivePrescription(ctx, t, pool, patientID, 1)
	require.Equal(t, http.StatusCreated, performCreateRequest(ctx, mux, createRequestBody(t, firstSlotID, patientID, professionalID, assistantID, nil)).Code)
	require.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, firstPrescriptionID))

	secondPrescriptionID := insertActivePrescription(ctx, t, pool, patientID, 1)
	require.Equal(t, http.StatusCreated, performCreateRequest(ctx, mux, createRequestBody(t, secondSlotID, patientID, professionalID, assistantID, nil)).Code)
	require.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, secondPrescriptionID))

	blockSlot(ctx, t, pool, firstSlotID)
	blockSlot(ctx, t, pool, secondSlotID)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	reconciled, err := repo.CancelOnBlockedSlots(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), reconciled)

	assert.Equal(t, 1, countActivePrescriptions(ctx, t, pool, patientID))
	// Ties go to the newest prescription, since the reopen visits IDs in
	// descending order and domain.NewID mints UUIDv7 values that sort by time.
	assert.Equal(t, prescriptionStatusActiveValue, fetchPrescriptionStatus(ctx, t, pool, secondPrescriptionID))
	assert.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, firstPrescriptionID))
}

// The compare-and-set is what makes status changes concurrency-safe, and it
// tells two failures apart: an appointment that does not exist, and one somebody
// else already moved. Both answers come from a read that now happens inside the
// update's own transaction — taking it from the pool instead would check out a
// second connection and hang wherever the pool is sized at one.
func TestUpdateStatusDistinguishesMissingFromConcurrentlyChanged(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)
	now := time.Now().UTC()

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	appointmentID := seedAppointmentForAction(ctx, t, pool, now.Add(30*time.Hour), now.Add(31*time.Hour), statusAbsentValue)

	// The row is ABSENT, so a transition expecting CONFIRMED matches nothing.
	err = repo.UpdateStatus(ctx, appointmentID, appointment.StatusCancelled, appointment.StatusConfirmed)
	assert.ErrorIs(t, err, appointment.ErrAppointmentStatusChanged)

	err = repo.UpdateStatus(ctx, uuid.NewV7(), appointment.StatusCancelled, appointment.StatusConfirmed)
	assert.ErrorIs(t, err, appointment.ErrInvalidAppointmentReference)

	status, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, appointmentID)
	assert.Equal(t, statusAbsentValue, status, "a rejected transition must leave the row untouched")
}

func blockSlot(ctx context.Context, t *testing.T, pool *pgxpool.Pool, slotID uuid.UUID) {
	t.Helper()

	_, err := pool.Exec(ctx, `UPDATE slot SET blocked = TRUE WHERE id = $1`, slotID)
	require.NoError(t, err)
}

// seedCompletedPrescriptionBooking books the single authorized session of a
// fresh prescription on a slot starting at start, leaving the prescription
// COMPLETED, and returns the pieces a reopen test needs.
func seedCompletedPrescriptionBooking(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	start time.Time,
) (patientID, prescriptionID, appointmentID uuid.UUID, mux *http.ServeMux) {
	t.Helper()

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	patientID = uuid.NewV7()
	slotID := uuid.NewV7()

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatientWithoutPrescription(ctx, t, pool, patientID)
	prescriptionID = insertActivePrescription(ctx, t, pool, patientID, 1)
	insertReopenSlot(ctx, t, pool, slotID, professionalID, start)

	mux = newIntegrationMux(t, pool)

	rec := performCreateRequest(ctx, mux, createRequestBody(t, slotID, patientID, professionalID, assistantID, nil))
	require.Equal(t, http.StatusCreated, rec.Code)
	appointmentID = appointmentIDFromLocation(t, rec.Header().Get("Location"))
	require.Equal(t, prescriptionStatusCompletedValue, fetchPrescriptionStatus(ctx, t, pool, prescriptionID))

	return patientID, prescriptionID, appointmentID, mux
}

func countActivePrescriptions(ctx context.Context, t *testing.T, pool *pgxpool.Pool, patientID uuid.UUID) int {
	t.Helper()

	var count int
	err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM prescription WHERE patient_id = $1 AND status = $2`,
		patientID,
		prescriptionStatusActiveValue,
	).Scan(&count)
	require.NoError(t, err)

	return count
}

func insertReopenSlot(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	slotID uuid.UUID,
	professionalID uuid.UUID,
	start time.Time,
) {
	t.Helper()

	date, startTime, endTime := slotValues(start, start.Add(30*time.Minute))
	insertSlot(ctx, t, pool, slotID, professionalID, date, startTime, endTime, reopenSlotCapacity, false)
}

func performCancelRequest(ctx context.Context, mux *http.ServeMux, appointmentID uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, appointmentsEndpoint+"/"+appointmentID.String()+"/cancel", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}
