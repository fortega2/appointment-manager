package prescription

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prescriptionFilePathRaw = "  prescriptions/laura.pdf  "
	prescriptionTotalRaw    = 10
	prescriptionWhitespace  = "   "
)

func TestNew(t *testing.T) {
	t.Parallel()

	patientID := uuid.Must(uuid.NewV7())

	created, err := New(patientID, prescriptionFilePathRaw, prescriptionTotalRaw)

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, patientID, created.PatientID)
	assert.Equal(t, "prescriptions/laura.pdf", created.FilePath)
	assert.Equal(t, prescriptionTotalRaw, created.TotalSessions)
	assert.Equal(t, StatusActive, created.Status)
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	patientID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name          string
		patientID     uuid.UUID
		filePath      string
		totalSessions int
		expected      error
	}{
		{
			name:          "nil patient id",
			patientID:     uuid.Nil,
			filePath:      "prescriptions/laura.pdf",
			totalSessions: 10,
			expected:      ErrNilPatientID,
		},
		{
			name:          "empty file path",
			patientID:     patientID,
			filePath:      "",
			totalSessions: 10,
			expected:      ErrEmptyFilePath,
		},
		{
			name:          "whitespace file path",
			patientID:     patientID,
			filePath:      prescriptionWhitespace,
			totalSessions: 10,
			expected:      ErrEmptyFilePath,
		},
		{
			name:          "zero sessions",
			patientID:     patientID,
			filePath:      "prescriptions/laura.pdf",
			totalSessions: 0,
			expected:      ErrInvalidTotalSessions,
		},
		{
			name:          "negative sessions",
			patientID:     patientID,
			filePath:      "prescriptions/laura.pdf",
			totalSessions: -3,
			expected:      ErrInvalidTotalSessions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := New(tt.patientID, tt.filePath, tt.totalSessions)

			require.Error(t, err)
			assert.Nil(t, created)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}
