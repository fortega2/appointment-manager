package session

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sessionUserID = "assistant-123"
	sessionEmail  = "assistant@email.com"
)

func TestStoreCreateAndGet(t *testing.T) {
	t.Parallel()

	store := &Store{sessions: make(map[string]*Session)}

	sessionID, err := store.Create(sessionUserID, sessionEmail)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	sessionValue, err := store.Get(sessionID)
	require.NoError(t, err)
	require.NotNil(t, sessionValue)
	assert.Equal(t, sessionUserID, sessionValue.UserID)
	assert.Equal(t, sessionEmail, sessionValue.Email)
	assert.False(t, sessionValue.CreatedAt.IsZero())
	assert.True(t, sessionValue.ExpiresAt.After(sessionValue.CreatedAt))
}

func TestStoreGetNotFound(t *testing.T) {
	t.Parallel()

	store := &Store{sessions: make(map[string]*Session)}

	sessionValue, err := store.Get("missing")
	require.Error(t, err)
	assert.Nil(t, sessionValue)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestStoreGetExpired(t *testing.T) {
	t.Parallel()

	store := &Store{sessions: map[string]*Session{
		"expired": {
			UserID:    sessionUserID,
			Email:     sessionEmail,
			CreatedAt: time.Now().Add(-2 * time.Hour),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		},
	}}

	sessionValue, err := store.Get("expired")
	require.Error(t, err)
	assert.Nil(t, sessionValue)
	assert.True(t, errors.Is(err, ErrSessionExpired))
}

func TestStoreDelete(t *testing.T) {
	t.Parallel()

	store := &Store{sessions: make(map[string]*Session)}

	sessionID, err := store.Create(sessionUserID, sessionEmail)
	require.NoError(t, err)

	store.Delete(sessionID)

	sessionValue, getErr := store.Get(sessionID)
	require.Error(t, getErr)
	assert.Nil(t, sessionValue)
	assert.True(t, errors.Is(getErr, ErrSessionNotFound))
}

func TestStoreGetReturnsCopy(t *testing.T) {
	t.Parallel()

	store := &Store{sessions: make(map[string]*Session)}

	sessionID, err := store.Create(sessionUserID, sessionEmail)
	require.NoError(t, err)

	sessionValue, err := store.Get(sessionID)
	require.NoError(t, err)

	sessionValue.Email = "mutated@email.com"

	fetchedAgain, err := store.Get(sessionID)
	require.NoError(t, err)
	assert.Equal(t, sessionEmail, fetchedAgain.Email)
}

func TestStoreCleanupRemovesExpiredOnly(t *testing.T) {
	t.Parallel()

	store := &Store{sessions: map[string]*Session{
		"expired": {
			UserID:    sessionUserID,
			Email:     sessionEmail,
			CreatedAt: time.Now().Add(-2 * time.Hour),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		},
		"active": {
			UserID:    sessionUserID,
			Email:     sessionEmail,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}}

	store.cleanup()

	_, expiredErr := store.Get("expired")
	require.Error(t, expiredErr)
	assert.True(t, errors.Is(expiredErr, ErrSessionNotFound))

	active, activeErr := store.Get("active")
	require.NoError(t, activeErr)
	require.NotNil(t, active)
}
