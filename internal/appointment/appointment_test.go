package appointment_test

import (
	"appointment-manager/internal/appointment"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAppointment(t *testing.T) {
	t.Parallel()

	slotID := uuid.NewV7()
	patientID := uuid.NewV7()
	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	notes := "follow-up"

	t.Run("with notes", func(t *testing.T) {
		t.Parallel()

		created := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, &notes)

		require.NotNil(t, created)
		assert.NotEqual(t, uuid.Nil(), created.ID)
		assert.Equal(t, slotID, created.SlotID)
		assert.Equal(t, patientID, created.PatientID)
		assert.Equal(t, professionalID, created.ProfessionalID)
		assert.Equal(t, assistantID, created.AssistantID)
		assert.Equal(t, appointment.StatusConfirmed, created.Status)
		require.NotNil(t, created.Notes)
		assert.Equal(t, notes, *created.Notes)
	})

	t.Run("without notes", func(t *testing.T) {
		t.Parallel()

		created := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, nil)

		require.NotNil(t, created)
		assert.Nil(t, created.Notes)
	})
}

func TestNewAppointmentCreatesUniqueID(t *testing.T) {
	t.Parallel()

	slotID := uuid.NewV7()
	patientID := uuid.NewV7()
	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()

	first := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, nil)
	second := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, nil)

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.NotEqual(t, first.ID, second.ID)
}

func TestStatusIsCancelled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status appointment.Status
		want   bool
	}{
		{name: "patient cancellation", status: appointment.StatusCancelled, want: true},
		{name: "clinic cancellation", status: appointment.StatusCancelledByClinic, want: true},
		{name: "confirmed", status: appointment.StatusConfirmed, want: false},
		{name: "absent", status: appointment.StatusAbsent, want: false},
		{name: "attended", status: appointment.StatusAttended, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.status.IsCancelled())
		})
	}
}

// The status values are pinned by the appointment_status lookup table that
// appointment.status references, so a drift here means writes fail on the
// foreign key rather than storing an unknown status.
func TestStatusValuesMatchLookupTable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, appointment.Status(1), appointment.StatusConfirmed)
	assert.Equal(t, appointment.Status(2), appointment.StatusCancelled)
	assert.Equal(t, appointment.Status(3), appointment.StatusAbsent)
	assert.Equal(t, appointment.Status(4), appointment.StatusAttended)
	assert.Equal(t, appointment.Status(5), appointment.StatusCancelledByClinic)
}
