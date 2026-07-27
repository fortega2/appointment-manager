package password

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const semaphoreTestValidEncodedHash = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"

func TestCompareRejectsWhenSemaphoreSaturated(t *testing.T) {
	t.Parallel()

	a := NewArgon2()
	for range maxConcurrentHashes {
		a.sem <- struct{}{}
	}

	ok, err := a.Compare(semaphoreTestValidEncodedHash, "irrelevant")

	assert.False(t, ok)
	require.ErrorIs(t, err, ErrTooManyConcurrentHashes)
}

func TestHashRejectsWhenSemaphoreSaturated(t *testing.T) {
	t.Parallel()

	a := NewArgon2()
	for range maxConcurrentHashes {
		a.sem <- struct{}{}
	}

	hash, err := a.Hash("irrelevant")

	assert.Empty(t, hash)
	require.ErrorIs(t, err, ErrTooManyConcurrentHashes)
}

func TestHashAndCompareReleaseSemaphoreSlot(t *testing.T) {
	t.Parallel()

	a := NewArgon2()
	const plain = "correct horse battery staple"

	hash, err := a.Hash(plain)
	require.NoError(t, err)
	assert.Empty(t, a.sem, "Hash must release its slot")

	_, err = a.Compare(hash, plain)
	require.NoError(t, err)
	assert.Empty(t, a.sem, "Compare must release its slot")
}
