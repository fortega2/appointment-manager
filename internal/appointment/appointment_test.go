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

	created := appointment.NewAppointment(slotID, patientID, professionalID, assistantID)

	require.NotNil(t, created)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, slotID, created.SlotID)
	assert.Equal(t, patientID, created.PatientID)
	assert.Equal(t, professionalID, created.ProfessionalID)
	assert.Equal(t, assistantID, created.AssistantID)
	assert.Equal(t, appointment.StatusConfirmed, created.Status)
}

func TestNewAppointmentCreatesUniqueID(t *testing.T) {
	t.Parallel()

	slotID := uuid.New()
	patientID := uuid.New()
	professionalID := uuid.New()
	assistantID := uuid.New()

	first := appointment.NewAppointment(slotID, patientID, professionalID, assistantID)
	second := appointment.NewAppointment(slotID, patientID, professionalID, assistantID)

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.NotEqual(t, first.ID, second.ID)
}
