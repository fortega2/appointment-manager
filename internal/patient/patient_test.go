package patient

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	patientFirstNameRaw       = "  Laura  "
	patientLastNameRaw        = "  Gomez  "
	patientPhoneRaw           = " 1133334444 "
	patientEmailRaw           = "  LAURA@MAIL.COM  "
	patientInsuranceNumberRaw = " 12345678901 "
	patientInsuranceID        = 1
	patientWhitespace         = "   "
)

func TestNewPatient(t *testing.T) {
	t.Parallel()

	notes := "  dolor lumbar  "

	created, err := NewPatient(
		patientFirstNameRaw,
		patientLastNameRaw,
		patientPhoneRaw,
		patientEmailRaw,
		patientInsuranceID,
		patientInsuranceNumberRaw,
		&notes,
	)

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, "Laura", created.FirstName)
	assert.Equal(t, "Gomez", created.LastName)
	assert.Equal(t, "1133334444", created.Phone)
	assert.Equal(t, "laura@mail.com", created.Email)
	assert.Equal(t, patientInsuranceID, created.HealthInsurance)
	assert.Equal(t, "12345678901", created.InsuranceNumber)
	require.NotNil(t, created.ClinicalNotes)
	assert.Equal(t, "dolor lumbar", *created.ClinicalNotes)
}

func TestNewPatientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		firstName       string
		lastName        string
		phone           string
		email           string
		healthInsurance int
		insuranceNumber string
		expected        error
	}{
		{
			name:            "first name required",
			firstName:       patientWhitespace,
			lastName:        "Gomez",
			phone:           "1133334444",
			email:           "laura@mail.com",
			healthInsurance: patientInsuranceID,
			insuranceNumber: "12345678901",
			expected:        ErrFirstNameRequired,
		},
		{
			name:            "last name required",
			firstName:       "Laura",
			lastName:        patientWhitespace,
			phone:           "1133334444",
			email:           "laura@mail.com",
			healthInsurance: patientInsuranceID,
			insuranceNumber: "12345678901",
			expected:        ErrLastNameRequired,
		},
		{
			name:            "phone required",
			firstName:       "Laura",
			lastName:        "Gomez",
			phone:           patientWhitespace,
			email:           "laura@mail.com",
			healthInsurance: patientInsuranceID,
			insuranceNumber: "12345678901",
			expected:        ErrPhoneRequired,
		},
		{
			name:            "email required",
			firstName:       "Laura",
			lastName:        "Gomez",
			phone:           "1133334444",
			email:           patientWhitespace,
			healthInsurance: patientInsuranceID,
			insuranceNumber: "12345678901",
			expected:        ErrEmailRequired,
		},
		{
			name:            "health insurance required",
			firstName:       "Laura",
			lastName:        "Gomez",
			phone:           "1133334444",
			email:           "laura@mail.com",
			healthInsurance: 0,
			insuranceNumber: "12345678901",
			expected:        ErrHealthInsuranceRequired,
		},
		{
			name:            "insurance number required",
			firstName:       "Laura",
			lastName:        "Gomez",
			phone:           "1133334444",
			email:           "laura@mail.com",
			healthInsurance: patientInsuranceID,
			insuranceNumber: patientWhitespace,
			expected:        ErrInsuranceNumberRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := NewPatient(
				tt.firstName,
				tt.lastName,
				tt.phone,
				tt.email,
				tt.healthInsurance,
				tt.insuranceNumber,
				nil,
			)

			require.Error(t, err)
			assert.Nil(t, created)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestNewPatientClinicalNotesNormalization(t *testing.T) {
	t.Parallel()

	blankNotes := patientWhitespace
	trimmedNotes := "  seguimiento mensual  "

	tests := []struct {
		name          string
		clinicalNotes *string
		expectedNil   bool
		expectedValue string
	}{
		{name: "nil notes", clinicalNotes: nil, expectedNil: true},
		{name: "blank notes", clinicalNotes: &blankNotes, expectedNil: true},
		{name: "trimmed notes", clinicalNotes: &trimmedNotes, expectedNil: false, expectedValue: "seguimiento mensual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := NewPatient(
				"Laura",
				"Gomez",
				"1133334444",
				"laura@mail.com",
				patientInsuranceID,
				"12345678901",
				tt.clinicalNotes,
			)

			require.NoError(t, err)
			require.NotNil(t, created)

			if tt.expectedNil {
				assert.Nil(t, created.ClinicalNotes)
				return
			}

			require.NotNil(t, created.ClinicalNotes)
			assert.Equal(t, tt.expectedValue, *created.ClinicalNotes)
		})
	}
}
