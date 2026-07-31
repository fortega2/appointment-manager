//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	cancelTargetSlotDate = "2999-06-01"
	cancelOtherSlotDate  = "2999-06-02"
)

// CancelBySlot flips only the CONFIRMED appointments of the given slot to
// CANCELLED, never touching other statuses, other slots, or the 24h rule that
// would otherwise mark a late cancellation as ABSENT.
func TestCancelBySlotCancelsOnlyConfirmedAppointmentsOfThatSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)

	// The unique partial index on (patient_id, slot_id) WHERE status = 1 forbids
	// two CONFIRMED appointments for the same patient in one slot, so every
	// confirmed booking below belongs to a distinct patient.
	firstPatientID := uuid.Must(uuid.NewV7())
	secondPatientID := uuid.Must(uuid.NewV7())
	attendedPatientID := uuid.Must(uuid.NewV7())
	cancelledPatientID := uuid.Must(uuid.NewV7())
	otherSlotPatientID := uuid.Must(uuid.NewV7())
	for _, patientID := range []uuid.UUID{
		firstPatientID,
		secondPatientID,
		attendedPatientID,
		cancelledPatientID,
		otherSlotPatientID,
	} {
		insertPatient(ctx, t, pool, patientID)
	}

	targetSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, targetSlotID, professionalID, cancelTargetSlotDate, "09:00:00+00", "09:30:00+00", 10, false)

	firstConfirmedID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, firstConfirmedID, targetSlotID, firstPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	secondConfirmedID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, secondConfirmedID, targetSlotID, secondPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	// Same slot, already ATTENDED -> untouched (not CONFIRMED).
	attendedID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, attendedID, targetSlotID, attendedPatientID, professionalID, assistantID, statusAttendedValue, nil)

	// Same slot, already CANCELLED -> untouched, and not double-counted.
	alreadyCancelledID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, alreadyCancelledID, targetSlotID, cancelledPatientID, professionalID, assistantID, statusCancelledValue, nil)

	// A different slot, CONFIRMED -> untouched.
	otherSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, otherSlotID, professionalID, cancelOtherSlotDate, "09:00:00+00", "09:30:00+00", 10, false)
	otherSlotApptID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, otherSlotApptID, otherSlotID, otherSlotPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.CancelBySlot(ctx, targetSlotID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	firstStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, firstConfirmedID)
	assert.Equal(t, statusCancelledValue, firstStatus)

	secondStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, secondConfirmedID)
	assert.Equal(t, statusCancelledValue, secondStatus)

	attendedStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, attendedID)
	assert.Equal(t, statusAttendedValue, attendedStatus)

	otherSlotStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, otherSlotApptID)
	assert.Equal(t, statusConfirmedValue, otherSlotStatus)

	// updated_at is stamped on the cancelled rows and left NULL on untouched ones.
	assert.NotNil(t, fetchAppointmentUpdatedAt(ctx, t, pool, firstConfirmedID))
	assert.Nil(t, fetchAppointmentUpdatedAt(ctx, t, pool, attendedID))
	assert.Nil(t, fetchAppointmentUpdatedAt(ctx, t, pool, otherSlotApptID))

	// Idempotent: a second cancel finds nothing left to cancel.
	secondCount, err := repo.CancelBySlot(ctx, targetSlotID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), secondCount)
}

// A slot with no bookings — and an id matching no slot at all — are ordinary
// zero-row results, not errors.
func TestCancelBySlotReturnsZeroWithoutError(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	insertProfessional(ctx, t, pool, professionalID)

	emptySlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, emptySlotID, professionalID, cancelTargetSlotDate, "11:00:00+00", "11:30:00+00", 5, false)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	emptyCount, err := repo.CancelBySlot(ctx, emptySlotID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), emptyCount)

	unknownCount, err := repo.CancelBySlot(ctx, uuid.Must(uuid.NewV7()))
	require.NoError(t, err)
	assert.Equal(t, int64(0), unknownCount)
}

// CancelOnBlockedSlots is the reconciliation sweep: it cancels appointments
// left CONFIRMED on a slot that is already blocked, and nothing else.
func TestCancelOnBlockedSlotsReconcilesConfirmedAppointments(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)

	blockedPatientID := uuid.Must(uuid.NewV7())
	blockedAttendedPatientID := uuid.Must(uuid.NewV7())
	openPatientID := uuid.Must(uuid.NewV7())
	for _, patientID := range []uuid.UUID{blockedPatientID, blockedAttendedPatientID, openPatientID} {
		insertPatient(ctx, t, pool, patientID)
	}

	// Blocked slot: the CONFIRMED booking is swept, the ATTENDED one is not.
	blockedSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, blockedSlotID, professionalID, cancelTargetSlotDate, "14:00:00+00", "14:30:00+00", 10, true)

	strandedID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, strandedID, blockedSlotID, blockedPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	blockedAttendedID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, blockedAttendedID, blockedSlotID, blockedAttendedPatientID, professionalID, assistantID, statusAttendedValue, nil)

	// Open slot: untouched, the sweep only targets blocked slots.
	openSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, openSlotID, professionalID, cancelOtherSlotDate, "14:00:00+00", "14:30:00+00", 10, false)
	openApptID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, openApptID, openSlotID, openPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.CancelOnBlockedSlots(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	strandedStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, strandedID)
	assert.Equal(t, statusCancelledValue, strandedStatus)

	blockedAttendedStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, blockedAttendedID)
	assert.Equal(t, statusAttendedValue, blockedAttendedStatus)

	openStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, openApptID)
	assert.Equal(t, statusConfirmedValue, openStatus)

	// Idempotent: nothing is left stranded after the first sweep.
	secondCount, err := repo.CancelOnBlockedSlots(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), secondCount)
}
