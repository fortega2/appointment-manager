package patient

import (
	"errors"
	"strings"
	"testing"
	"uuid"

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

	patientFirstName       = "Laura"
	patientLastName        = "Gomez"
	patientPhone           = "1133334444"
	patientEmail           = "laura@mail.com"
	patientInsuranceNumber = "12345678901"
)

type patientInput struct {
	firstName       string
	lastName        string
	phone           string
	email           string
	healthInsurance int
	insuranceNumber string
	clinicalNotes   *string
}

func validPatientInput() patientInput {
	return patientInput{
		firstName:       patientFirstName,
		lastName:        patientLastName,
		phone:           patientPhone,
		email:           patientEmail,
		healthInsurance: patientInsuranceID,
		insuranceNumber: patientInsuranceNumber,
	}
}

func (in patientInput) create() (*Patient, error) {
	return NewPatient(in.firstName, in.lastName, in.phone, in.email, in.healthInsurance, in.insuranceNumber, in.clinicalNotes)
}

func (in patientInput) applyTo(p *Patient) error {
	return p.Update(in.firstName, in.lastName, in.phone, in.email, in.healthInsurance, in.insuranceNumber, in.clinicalNotes)
}

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
	assert.NotEqual(t, uuid.Nil(), created.ID)
	assert.Equal(t, "Laura", created.FirstName)
	assert.Equal(t, "Gomez", created.LastName)
	assert.Equal(t, "1133334444", created.Phone)
	assert.Equal(t, "laura@mail.com", created.Email)
	assert.Equal(t, patientInsuranceID, created.HealthInsurance)
	assert.Equal(t, "12345678901", created.InsuranceNumber)
	require.NotNil(t, created.ClinicalNotes)
	assert.Equal(t, "dolor lumbar", *created.ClinicalNotes)
}

// TestPatientValidationIsIdenticalForCreateAndUpdate drives one table of invalid
// inputs through both NewPatient and Update.
//
// Update used to be a hand-copied duplicate of NewPatient that had lost the
// insurance-number length check, so an over-long value slipped past the domain,
// hit the char(11) column and surfaced to the user as an opaque 500. Asserting
// both paths against the same cases is what stops them drifting apart again: a
// rule added to only one of them fails here.
func TestPatientValidationIsIdenticalForCreateAndUpdate(t *testing.T) {
	t.Parallel()

	overlongName := strings.Repeat("a", maxNameLength+1)

	tests := []struct {
		name     string
		mutate   func(*patientInput)
		expected error
	}{
		{
			name:     "first name required",
			mutate:   func(in *patientInput) { in.firstName = patientWhitespace },
			expected: ErrFirstNameRequired,
		},
		{
			name:     "first name too long",
			mutate:   func(in *patientInput) { in.firstName = overlongName },
			expected: ErrFirstNameTooLong,
		},
		{
			name:     "last name required",
			mutate:   func(in *patientInput) { in.lastName = patientWhitespace },
			expected: ErrLastNameRequired,
		},
		{
			name:     "last name too long",
			mutate:   func(in *patientInput) { in.lastName = overlongName },
			expected: ErrLastNameTooLong,
		},
		{
			name:     "phone required",
			mutate:   func(in *patientInput) { in.phone = patientWhitespace },
			expected: ErrPhoneRequired,
		},
		{
			name:     "email required",
			mutate:   func(in *patientInput) { in.email = patientWhitespace },
			expected: ErrEmailRequired,
		},
		{
			name:     "email too long",
			mutate:   func(in *patientInput) { in.email = strings.Repeat("a", maxEmailLength) + "@mail.com" },
			expected: ErrEmailTooLong,
		},
		{
			name:     "health insurance required",
			mutate:   func(in *patientInput) { in.healthInsurance = 0 },
			expected: ErrHealthInsuranceRequired,
		},
		{
			name:     "insurance number required",
			mutate:   func(in *patientInput) { in.insuranceNumber = patientWhitespace },
			expected: ErrInsuranceNumberRequired,
		},
		{
			name:     "insurance number too short",
			mutate:   func(in *patientInput) { in.insuranceNumber = strings.Repeat("1", insuranceNumberLength-1) },
			expected: ErrInvalidInsuranceNumberLength,
		},
		{
			name:     "insurance number too long",
			mutate:   func(in *patientInput) { in.insuranceNumber = strings.Repeat("1", insuranceNumberLength+1) },
			expected: ErrInvalidInsuranceNumberLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := validPatientInput()
			tt.mutate(&input)

			t.Run("create", func(t *testing.T) {
				t.Parallel()

				created, err := input.create()

				require.Error(t, err)
				assert.Nil(t, created)
				assert.True(t, errors.Is(err, tt.expected), "got %v, want %v", err, tt.expected)
			})

			t.Run("update", func(t *testing.T) {
				t.Parallel()

				existing, err := validPatientInput().create()
				require.NoError(t, err)

				err = input.applyTo(existing)

				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expected), "got %v, want %v", err, tt.expected)
			})
		})
	}
}

func TestPatientInsuranceNumberLengthBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		digits  int
		wantErr bool
	}{
		{name: "one short", digits: insuranceNumberLength - 1, wantErr: true},
		{name: "exact", digits: insuranceNumberLength, wantErr: false},
		{name: "one over", digits: insuranceNumberLength + 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := validPatientInput()
			input.insuranceNumber = strings.Repeat("1", tt.digits)

			created, err := input.create()

			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidInsuranceNumberLength)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
		})
	}
}

// TestPatientLengthLimitsCountRunesNotBytes pins the unit the limits are measured
// in. Postgres counts characters for varchar(n), so a 255-character name of
// two-byte runes fits the column; measuring with len() would reject it at 510
// bytes and lock legitimate Spanish surnames out of the system.
func TestPatientLengthLimitsCountRunesNotBytes(t *testing.T) {
	t.Parallel()

	input := validPatientInput()
	input.lastName = strings.Repeat("ñ", maxNameLength)

	created, err := input.create()

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, maxNameLength, len([]rune(created.LastName)))
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

func TestPatientUpdate(t *testing.T) {
	t.Parallel()

	existing, err := validPatientInput().create()
	require.NoError(t, err)
	originalID := existing.ID

	notes := "  control post operatorio  "
	err = existing.Update(
		patientFirstNameRaw,
		patientLastNameRaw,
		patientPhoneRaw,
		patientEmailRaw,
		patientInsuranceID+1,
		patientInsuranceNumberRaw,
		&notes,
	)

	require.NoError(t, err)
	assert.Equal(t, originalID, existing.ID, "update must not reassign the identity")
	assert.Equal(t, patientFirstName, existing.FirstName)
	assert.Equal(t, patientLastName, existing.LastName)
	assert.Equal(t, patientPhone, existing.Phone)
	assert.Equal(t, patientEmail, existing.Email)
	assert.Equal(t, patientInsuranceID+1, existing.HealthInsurance)
	assert.Equal(t, patientInsuranceNumber, existing.InsuranceNumber)
	require.NotNil(t, existing.ClinicalNotes)
	assert.Equal(t, "control post operatorio", *existing.ClinicalNotes)
}

func TestPatientUpdateClearsClinicalNotes(t *testing.T) {
	t.Parallel()

	notes := "dolor lumbar"
	input := validPatientInput()
	input.clinicalNotes = &notes

	blank := patientWhitespace
	tests := []struct {
		name          string
		clinicalNotes *string
	}{
		{name: "nil notes", clinicalNotes: nil},
		{name: "blank notes", clinicalNotes: &blank},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updated, err := input.create()
			require.NoError(t, err)
			require.NotNil(t, updated.ClinicalNotes, "precondition: the patient starts with notes")

			cleared := validPatientInput()
			cleared.clinicalNotes = tt.clinicalNotes
			require.NoError(t, cleared.applyTo(updated))

			assert.Nil(t, updated.ClinicalNotes)
		})
	}
}

// TestPatientUpdateDoesNotMutateOnError guards against a partially applied
// update: validation runs to completion before any field is written, so a
// rejected form leaves the in-memory patient exactly as it was loaded.
func TestPatientUpdateDoesNotMutateOnError(t *testing.T) {
	t.Parallel()

	existing, err := validPatientInput().create()
	require.NoError(t, err)
	before := *existing

	// First name is valid here and comes first in the validator; the insurance
	// number is what fails. A validator that assigned as it went would already
	// have overwritten the name by the time it rejected the input.
	input := validPatientInput()
	input.firstName = "Mariana"
	input.insuranceNumber = strings.Repeat("9", insuranceNumberLength+1)

	err = input.applyTo(existing)

	require.ErrorIs(t, err, ErrInvalidInsuranceNumberLength)
	assert.Equal(t, before, *existing)
}
