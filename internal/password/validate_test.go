package password_test

import (
	"appointment-manager/internal/password"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	asciiChar = "a"
	multiByte = "🔑"
	spaceChar = " "
)

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plain    string
		expected error
	}{
		{
			name:     "empty",
			plain:    "",
			expected: password.ErrPasswordTooShort,
		},
		{
			name:     "one short of the minimum",
			plain:    strings.Repeat(asciiChar, password.MinLength-1),
			expected: password.ErrPasswordTooShort,
		},
		{
			name:     "exactly the minimum",
			plain:    strings.Repeat(asciiChar, password.MinLength),
			expected: nil,
		},
		{
			name:     "exactly the maximum",
			plain:    strings.Repeat(asciiChar, password.MaxLength),
			expected: nil,
		},
		{
			name:     "one past the maximum",
			plain:    strings.Repeat(asciiChar, password.MaxLength+1),
			expected: password.ErrPasswordTooLong,
		},
		{
			name:     "a passphrase with spaces",
			plain:    "correct horse battery staple",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := password.Validate(tt.plain)
			if tt.expected == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

// TestValidateCountsRunesNotBytes is the reason Validate does not use len():
// four emoji clear a byte-counted minimum of twelve while being four characters.
func TestValidateCountsRunesNotBytes(t *testing.T) {
	t.Parallel()

	short := strings.Repeat(multiByte, 4)
	require.Greater(t, len(short), password.MinLength)
	assert.ErrorIs(t, password.Validate(short), password.ErrPasswordTooShort)

	long := strings.Repeat(multiByte, password.MinLength)
	require.NoError(t, password.Validate(long))
}

// TestValidateDoesNotTrim pins that whitespace counts: trimming would silently
// accept a password the login would then reject.
func TestValidateDoesNotTrim(t *testing.T) {
	t.Parallel()

	padded := strings.Repeat(spaceChar, password.MinLength)
	assert.NoError(t, password.Validate(padded))
}
