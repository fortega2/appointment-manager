package assistant_test

import (
	"appointment-manager/internal/assistant"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	assistantNames          = "Jane"
	assistantLastNames      = "Doe"
	assistantEmail          = "jane.doe@email.com"
	assistantHashedPassword = "hashed-password"
	assistantHash           = "hash"
	invalidAssistantID      = "not-an-id"
	whitespaceLiteral       = "   "
	twoSpacesLiteral        = "  "
)

func TestNewAssistant(t *testing.T) {
	t.Parallel()

	assistantRecord, err := assistant.NewAssistant(assistantNames, assistantLastNames, assistantEmail, assistantHashedPassword)

	require.NoError(t, err)
	require.NotNil(t, assistantRecord)
	assert.NotEmpty(t, assistantRecord.ID)
	assert.Equal(t, assistantNames, assistantRecord.FirstName)
	assert.Equal(t, assistantLastNames, assistantRecord.LastName)
	assert.Equal(t, assistantEmail, assistantRecord.Email)
	assert.Equal(t, assistantHashedPassword, assistantRecord.PasswordHash)
}

func TestNewAssistantValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		names        string
		lastNames    string
		email        string
		passwordHash string
		expectedErr  error
	}{
		{
			name:         "missing names",
			names:        whitespaceLiteral,
			lastNames:    assistantLastNames,
			email:        assistantEmail,
			passwordHash: assistantHash,
			expectedErr:  assistant.ErrAssistantRequestFirstNameRequired,
		},
		{
			name:         "missing last names",
			names:        assistantNames,
			lastNames:    whitespaceLiteral,
			email:        assistantEmail,
			passwordHash: assistantHash,
			expectedErr:  assistant.ErrAssistantRequestLastNameRequired,
		},
		{
			name:         "missing email",
			names:        assistantNames,
			lastNames:    assistantLastNames,
			email:        "",
			passwordHash: assistantHash,
			expectedErr:  assistant.ErrAssistantRequestEmailRequired,
		},
		{
			name:         "missing password",
			names:        assistantNames,
			lastNames:    assistantLastNames,
			email:        assistantEmail,
			passwordHash: twoSpacesLiteral,
			expectedErr:  assistant.ErrAssistantRequestPasswordRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assistantRecord, err := assistant.NewAssistant(tt.names, tt.lastNames, tt.email, tt.passwordHash)

			require.Error(t, err)
			assert.Nil(t, assistantRecord)
			assert.True(t, errors.Is(err, tt.expectedErr))
		})
	}
}

func TestParseID(t *testing.T) {
	t.Parallel()

	assistantRecord, err := assistant.NewAssistant(assistantNames, assistantLastNames, assistantEmail, assistantHash)
	require.NoError(t, err)

	parsedID, parseErr := assistant.ParseID(assistantRecord.ID.String())

	require.NoError(t, parseErr)
	assert.Equal(t, assistantRecord.ID, parsedID)
}

func TestParseIDValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		expected error
	}{
		{name: "empty", raw: "", expected: assistant.ErrInvalidID},
		{name: "invalid uuid", raw: invalidAssistantID, expected: assistant.ErrInvalidID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsedID, err := assistant.ParseID(tt.raw)

			require.Error(t, err)
			assert.Equal(t, uuid.Nil, parsedID)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}
