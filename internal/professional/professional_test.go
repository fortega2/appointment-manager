package professional

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	professionalFirstName = "Laura"
	professionalLastName  = "Gomez"
	professionalPhone     = "1133334444"
	whitespaceValue       = "   "

	defaultSpecialty = "kinesiology"
)

func TestNewProfessional(t *testing.T) {
	t.Parallel()

	created, err := NewProfessional(professionalFirstName, professionalLastName, professionalPhone)

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, professionalFirstName, created.FirstName)
	assert.Equal(t, professionalLastName, created.LastName)
	assert.Equal(t, professionalPhone, created.Phone)
	assert.Equal(t, defaultSpecialty, created.Specialty)
	assert.True(t, created.Active)
}

func TestNewProfessionalValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		firstName string
		lastName  string
		phone     string
		expected  error
	}{
		{
			name:      "first name required",
			firstName: whitespaceValue,
			lastName:  professionalLastName,
			phone:     professionalPhone,
			expected:  ErrFirstNameRequired,
		},
		{
			name:      "last name required",
			firstName: professionalFirstName,
			lastName:  whitespaceValue,
			phone:     professionalPhone,
			expected:  ErrLastNameRequired,
		},
		{
			name:      "phone required",
			firstName: professionalFirstName,
			lastName:  professionalLastName,
			phone:     whitespaceValue,
			expected:  ErrPhoneRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := NewProfessional(tt.firstName, tt.lastName, tt.phone)

			require.Error(t, err)
			assert.Nil(t, created)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}
