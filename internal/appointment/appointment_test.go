package appointment_test

import (
	"appointment-manager/internal/appointment"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAppointment(t *testing.T) {
	t.Parallel()

	slotID := uuid.New()
	patientID := uuid.New()
	professionalID := uuid.New()
	assistantID := uuid.New()
	notes := "follow-up"

	t.Run("with notes", func(t *testing.T) {
		t.Parallel()

		created := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, &notes)

		require.NotNil(t, created)
		assert.NotEqual(t, uuid.Nil, created.ID)
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

	slotID := uuid.New()
	patientID := uuid.New()
	professionalID := uuid.New()
	assistantID := uuid.New()

	first := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, nil)
	second := appointment.NewAppointment(slotID, patientID, professionalID, assistantID, nil)

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.NotEqual(t, first.ID, second.ID)
}
