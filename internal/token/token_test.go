package token_test

import (
	"appointment-manager/internal/token"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tokenBytes = 32
	digestLen  = 64

	// sha256 of "token" and of the empty string, so a change of algorithm or
	// encoding fails here rather than silently invalidating every stored digest.
	knownDigest = "3c469e9d6c5875d37a43f353d4f88e61fcf812c66eee3457465a40b0da4153e0"
	emptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected error
		name     string
		size     uint
	}{
		{
			name:     "zero bytes",
			size:     0,
			expected: token.ErrZeroBytes,
		},
		{
			name:     "non-zero bytes",
			size:     tokenBytes,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			generated, err := token.Generate(tt.size)
			if tt.expected == nil {
				require.NoError(t, err)
				require.NotEmpty(t, generated)

				return
			}

			require.Empty(t, generated)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestGenerateEncodesRequestedBytes(t *testing.T) {
	t.Parallel()

	for _, size := range []uint{1, 2, 3, tokenBytes, 255} {
		t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
			t.Parallel()

			generated, err := token.Generate(size)
			require.NoError(t, err)

			// Decoding with the URL alphabet is also what pins it: a token full of
			// "+" or "/" would not survive the query string the reset link uses.
			decoded, err := base64.URLEncoding.DecodeString(generated)
			require.NoError(t, err)
			assert.Len(t, decoded, int(size))
		})
	}
}

func TestGenerateProducesDistinctValues(t *testing.T) {
	t.Parallel()

	first, err := token.Generate(tokenBytes)
	require.NoError(t, err)

	second, err := token.Generate(tokenBytes)
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "known value",
			input:    "token",
			expected: knownDigest,
		},
		{
			name:     "empty value",
			input:    "",
			expected: emptyDigest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, token.Hash(tt.input))
		})
	}
}

func TestHashIsDeterministicAndInputSensitive(t *testing.T) {
	t.Parallel()

	first := token.Hash("first")
	second := token.Hash("first")
	different := token.Hash("second")

	assert.Equal(t, first, second)
	assert.NotEqual(t, first, different)
	assert.Len(t, first, digestLen)
}
