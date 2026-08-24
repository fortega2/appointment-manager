package session

import (
	"appointment-manager/internal/token"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sessionUserID = "assistant-123"
	otherUserID   = "assistant-456"
)

var errStorerFailed = errors.New("storer failed")

type stubStorer struct {
	sessions       map[string]Session
	createErr      error
	deleteErr      error
	byAssistantErr error
	expiredErr     error
	lastBefore     time.Time
}

func newStubStorer() *stubStorer {
	return &stubStorer{sessions: make(map[string]Session)}
}

func (s *stubStorer) Create(_ context.Context, session Session) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.sessions[session.ID] = session

	return nil
}

func (s *stubStorer) Get(_ context.Context, id string) (*Session, error) {
	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	copied := session

	return &copied, nil
}

func (s *stubStorer) Delete(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if _, ok := s.sessions[id]; !ok {
		return ErrSessionNotFound
	}
	delete(s.sessions, id)

	return nil
}

func (s *stubStorer) DeleteByAssistant(_ context.Context, userID string) (int64, error) {
	if s.byAssistantErr != nil {
		return 0, s.byAssistantErr
	}

	removed := int64(0)
	for id, session := range s.sessions {
		if session.UserID == userID {
			delete(s.sessions, id)
			removed++
		}
	}

	return removed, nil
}

func (s *stubStorer) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	if s.expiredErr != nil {
		return 0, s.expiredErr
	}
	s.lastBefore = before

	removed := int64(0)
	for id, session := range s.sessions {
		if before.After(session.ExpiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}

	return removed, nil
}

func newTestStore(t *testing.T, storer Storer) *Store {
	t.Helper()

	store, err := NewStore(storer)
	require.NoError(t, err)

	return store
}

func TestNewStoreValidation(t *testing.T) {
	t.Parallel()

	store, err := NewStore(nil)
	require.Error(t, err)
	assert.Nil(t, store)
	assert.ErrorIs(t, err, ErrNilStorer)
}

func TestStoreCreateAndGet(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	token, err := store.Create(t.Context(), sessionUserID)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	sessionValue, err := store.Get(t.Context(), token)
	require.NoError(t, err)
	require.NotNil(t, sessionValue)
	assert.Equal(t, sessionUserID, sessionValue.UserID)
	assert.False(t, sessionValue.CreatedAt.IsZero())
	assert.True(t, sessionValue.ExpiresAt.After(sessionValue.CreatedAt))
}

func TestStoreCreatePersistsDigestNotToken(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	store := newTestStore(t, storer)

	tken, err := store.Create(t.Context(), sessionUserID)
	require.NoError(t, err)

	require.Len(t, storer.sessions, 1)
	_, storedUnderToken := storer.sessions[tken]
	assert.False(t, storedUnderToken, "the raw cookie token must never be the stored key")

	stored, ok := storer.sessions[token.Hash(tken)]
	require.True(t, ok)
	assert.Len(t, stored.ID, 64)
	assert.NotEqual(t, tken, stored.ID)
}

func TestStoreCreateStorerError(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.createErr = errStorerFailed
	store := newTestStore(t, storer)

	token, err := store.Create(t.Context(), sessionUserID)
	require.Error(t, err)
	assert.Empty(t, token)
	assert.ErrorIs(t, err, errStorerFailed)
}

func TestStoreGetNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	sessionValue, err := store.Get(t.Context(), "missing")
	require.Error(t, err)
	assert.Nil(t, sessionValue)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestStoreGetOtherTokenDoesNotMatch(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	token, err := store.Create(t.Context(), sessionUserID)
	require.NoError(t, err)

	sessionValue, err := store.Get(t.Context(), token+"tampered")
	require.Error(t, err)
	assert.Nil(t, sessionValue)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestStoreGetExpired(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.sessions[token.Hash("expired")] = Session{
		ID:        token.Hash("expired"),
		UserID:    sessionUserID,
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	store := newTestStore(t, storer)

	sessionValue, err := store.Get(t.Context(), "expired")
	require.Error(t, err)
	assert.Nil(t, sessionValue)
	assert.ErrorIs(t, err, ErrSessionExpired)
}

func TestStoreDelete(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	token, err := store.Create(t.Context(), sessionUserID)
	require.NoError(t, err)

	require.NoError(t, store.Delete(t.Context(), token))

	sessionValue, getErr := store.Get(t.Context(), token)
	require.Error(t, getErr)
	assert.Nil(t, sessionValue)
	assert.ErrorIs(t, getErr, ErrSessionNotFound)
}

func TestStoreDeletePropagatesStorerError(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.deleteErr = errStorerFailed
	store := newTestStore(t, storer)

	err := store.Delete(t.Context(), "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, errStorerFailed)
}

func TestStoreDeleteByAssistant(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	first, err := store.Create(t.Context(), sessionUserID)
	require.NoError(t, err)
	second, err := store.Create(t.Context(), sessionUserID)
	require.NoError(t, err)
	untouched, err := store.Create(t.Context(), otherUserID)
	require.NoError(t, err)

	removed, err := store.DeleteByAssistant(t.Context(), sessionUserID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)

	for _, token := range []string{first, second} {
		_, getErr := store.Get(t.Context(), token)
		assert.ErrorIs(t, getErr, ErrSessionNotFound)
	}

	survivor, err := store.Get(t.Context(), untouched)
	require.NoError(t, err)
	assert.Equal(t, otherUserID, survivor.UserID)
}

// TestStoreDeleteByAssistantWithoutSessions pins that an assistant logged in
// nowhere is a clean result, not ErrSessionNotFound.
func TestStoreDeleteByAssistantWithoutSessions(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	removed, err := store.DeleteByAssistant(t.Context(), sessionUserID)
	require.NoError(t, err)
	assert.Zero(t, removed)
}

func TestStoreDeleteByAssistantStorerError(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.byAssistantErr = errStorerFailed
	store := newTestStore(t, storer)

	removed, err := store.DeleteByAssistant(t.Context(), sessionUserID)
	require.Error(t, err)
	assert.Zero(t, removed)
	assert.ErrorIs(t, err, errStorerFailed)
}

func TestStoreDeleteExpired(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.sessions["expired"] = Session{
		ID:        "expired",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	storer.sessions["active"] = Session{
		ID:        "active",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	store := newTestStore(t, storer)

	removed, err := store.DeleteExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
	assert.False(t, storer.lastBefore.IsZero())

	_, expiredFound := storer.sessions["expired"]
	assert.False(t, expiredFound)
	_, activeFound := storer.sessions["active"]
	assert.True(t, activeFound)
}

func TestStoreDeleteExpiredStorerError(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.expiredErr = errStorerFailed
	store := newTestStore(t, storer)

	removed, err := store.DeleteExpired(t.Context())
	require.Error(t, err)
	assert.Zero(t, removed)
	assert.ErrorIs(t, err, errStorerFailed)
}
