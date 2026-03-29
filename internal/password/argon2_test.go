package password_test

import (
	"appointment-manager/internal/password"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	passwordSuperSecret              = "super-secret"
	passwordWrong                    = "wrong-password"
	passwordGeneric                  = "password"
	argon2Prefix                     = "$argon2id$v="
	errInvalidEncodedHashFormat      = "invalid encoded hash format"
	errInvalidAlgorithm              = "invalid algorithm"
	errIncompatibleArgonVersion      = "incompatible argon2 version"
	errInvalidMemoryCost             = "invalid memory cost"
	errDecodeSalt                    = "decode salt"
	errDecodeHash                    = "decode hash"
	caseInvalidFormat                = "invalid format"
	caseInvalidAlgorithm             = "invalid algorithm"
	caseIncompatibleVersion          = "incompatible version"
	caseInvalidParams                = "invalid params"
	caseInvalidSaltBase64            = "invalid salt base64"
	caseInvalidHashBase64            = "invalid hash base64"
	encodedInvalidFiveParts          = "$argon2id$v=19$m=65536,t=3,p=2$invalid-only-five-parts"
	encodedInvalidAlgorithm          = "$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"
	encodedIncompatibleVersion       = "$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$aGFzaA"
	encodedInvalidParams             = "$argon2id$v=19$m=0,t=3,p=2$c2FsdA$aGFzaA"
	encodedInvalidSaltBase64         = "$argon2id$v=19$m=65536,t=3,p=2$%%%$aGFzaA"
	encodedInvalidHashBase64         = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$%%%"
	encodedTooLargeHash              = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$w0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDS8NLw0vDSw"
	errHashLengthExceedsMaximum      = "hash length exceeds maximum"
	generatedHashShouldBeUnique      = "generated hashes should be unique due to random salt"
	generatedHashShouldContainPrefix = "hash should include argon2 PHC prefix"
)

func TestHashAndCompare(t *testing.T) {
	t.Parallel()

	hasher := password.NewArgon2()

	encodedHash, err := hasher.Hash(passwordSuperSecret)
	require.NoError(t, err)
	assert.NotEmpty(t, encodedHash)
	assert.Contains(t, encodedHash, argon2Prefix, generatedHashShouldContainPrefix)

	match, compareErr := hasher.Compare(encodedHash, passwordSuperSecret)
	require.NoError(t, compareErr)
	assert.True(t, match)

	noMatch, wrongCompareErr := hasher.Compare(encodedHash, passwordWrong)
	require.NoError(t, wrongCompareErr)
	assert.False(t, noMatch)
}

func TestHashUsesRandomSalt(t *testing.T) {
	t.Parallel()

	hasher := password.NewArgon2()

	hashA, errA := hasher.Hash(passwordGeneric)
	require.NoError(t, errA)

	hashB, errB := hasher.Hash(passwordGeneric)
	require.NoError(t, errB)

	assert.NotEqual(t, hashA, hashB, generatedHashShouldBeUnique)
}

func TestCompareErrors(t *testing.T) {
	t.Parallel()

	hasher := password.NewArgon2()

	tests := []struct {
		name        string
		encodedHash string
		expectedErr string
	}{
		{name: caseInvalidFormat, encodedHash: encodedInvalidFiveParts, expectedErr: errInvalidEncodedHashFormat},
		{name: caseInvalidAlgorithm, encodedHash: encodedInvalidAlgorithm, expectedErr: errInvalidAlgorithm},
		{name: caseIncompatibleVersion, encodedHash: encodedIncompatibleVersion, expectedErr: errIncompatibleArgonVersion},
		{name: caseInvalidParams, encodedHash: encodedInvalidParams, expectedErr: errInvalidMemoryCost},
		{name: caseInvalidSaltBase64, encodedHash: encodedInvalidSaltBase64, expectedErr: errDecodeSalt},
		{name: caseInvalidHashBase64, encodedHash: encodedInvalidHashBase64, expectedErr: errDecodeHash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			match, err := hasher.Compare(tt.encodedHash, passwordSuperSecret)

			require.Error(t, err)
			assert.False(t, match)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestCompareHashLengthLimit(t *testing.T) {
	t.Parallel()

	hasher := password.NewArgon2()

	match, err := hasher.Compare(encodedTooLargeHash, passwordGeneric)

	require.Error(t, err)
	assert.False(t, match)
	assert.Contains(t, err.Error(), errHashLengthExceedsMaximum)
}
