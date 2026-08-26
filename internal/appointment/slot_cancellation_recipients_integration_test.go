//go:build integration

package appointment_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"appointment-manager/internal/appointment"
)

const (
	recipientsSlotDate      = "2999-07-01"
	recipientsOtherSlotDate = "2999-07-02"
)

// The query answers "who did the clinic call off", which is not the same
// question as "who is cancelled on this slot". A patient who cancelled their
// own appointment must not be told the clinic cancelled it.
func TestSlotCancellationRecipientsExcludesPatientCancellations(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)

	clinicCancelledPatientID := uuid.NewV7()
	selfCancelledPatientID := uuid.NewV7()
	attendedPatientID := uuid.NewV7()
	otherSlotPatientID := uuid.NewV7()
	for _, patientID := range []uuid.UUID{
		clinicCancelledPatientID,
		selfCancelledPatientID,
		attendedPatientID,
		otherSlotPatientID,
	} {
		insertPatient(ctx, t, pool, patientID)
	}

	slotID := uuid.NewV7()
	insertSlot(ctx, t, pool, slotID, professionalID, recipientsSlotDate, "09:00:00+00", "09:30:00+00", 10, true)

	// The clinic called this one off -> must be notified.
	insertAppointment(ctx, t, pool, uuid.NewV7(), slotID, clinicCancelledPatientID,
		professionalID, assistantID, statusCancelledByClinicValue, nil)

	// This patient cancelled themselves days earlier -> must NOT be notified.
	insertAppointment(ctx, t, pool, uuid.NewV7(), slotID, selfCancelledPatientID,
		professionalID, assistantID, statusCancelledValue, nil)

	// Same slot but already attended -> nothing to announce.
	insertAppointment(ctx, t, pool, uuid.NewV7(), slotID, attendedPatientID,
		professionalID, assistantID, statusAttendedValue, nil)

	// A different slot the clinic also cancelled -> must not leak in.
	otherSlotID := uuid.NewV7()
	insertSlot(ctx, t, pool, otherSlotID, professionalID, recipientsOtherSlotDate, "09:00:00+00", "09:30:00+00", 10, true)
	insertAppointment(ctx, t, pool, uuid.NewV7(), otherSlotID, otherSlotPatientID,
		professionalID, assistantID, statusCancelledByClinicValue, nil)

	query, err := appointment.NewQuery(pool)
	require.NoError(t, err)

	cancellation, err := query.SlotCancellationRecipients(ctx, slotID)
	require.NoError(t, err)

	require.Len(t, cancellation.Recipients, 1, "only the clinic-cancelled appointment of this slot")
	assert.Equal(t, clinicCancelledPatientID.String(), cancellation.Recipients[0].PatientID)

	// The slot details travel with the group, so a message can name the booking
	// the patient lost.
	assert.False(t, cancellation.StartTime.IsZero())
	assert.True(t, cancellation.EndTime.After(cancellation.StartTime))
	assert.NotEmpty(t, cancellation.ProfessionalFullName)
}

// patient.email is nullable while phone is NOT NULL, so a patient with no
// address on file must scan cleanly rather than failing the whole lookup.
func TestSlotCancellationRecipientsHandlesNullEmail(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)

	patientID := uuid.NewV7()
	insertPatient(ctx, t, pool, patientID)
	_, err := pool.Exec(ctx, `UPDATE public.patient SET email = NULL WHERE id = $1`, patientID)
	require.NoError(t, err)

	slotID := uuid.NewV7()
	insertSlot(ctx, t, pool, slotID, professionalID, recipientsSlotDate, "11:00:00+00", "11:30:00+00", 5, true)
	insertAppointment(ctx, t, pool, uuid.NewV7(), slotID, patientID,
		professionalID, assistantID, statusCancelledByClinicValue, nil)

	query, err := appointment.NewQuery(pool)
	require.NoError(t, err)

	cancellation, err := query.SlotCancellationRecipients(ctx, slotID)
	require.NoError(t, err)

	require.Len(t, cancellation.Recipients, 1)
	assert.Nil(t, cancellation.Recipients[0].Email, "a missing address is absent, not empty")
	assert.NotEmpty(t, cancellation.Recipients[0].Phone)
}

// A slot nobody booked is an ordinary outcome: an empty result, not an error.
func TestSlotCancellationRecipientsReturnsEmptyWithoutError(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.NewV7()
	insertProfessional(ctx, t, pool, professionalID)

	emptySlotID := uuid.NewV7()
	insertSlot(ctx, t, pool, emptySlotID, professionalID, recipientsSlotDate, "13:00:00+00", "13:30:00+00", 5, true)

	query, err := appointment.NewQuery(pool)
	require.NoError(t, err)

	cancellation, err := query.SlotCancellationRecipients(ctx, emptySlotID)
	require.NoError(t, err)
	assert.Empty(t, cancellation.Recipients)

	unknown, err := query.SlotCancellationRecipients(ctx, uuid.NewV7())
	require.NoError(t, err)
	assert.Empty(t, unknown.Recipients)
}
