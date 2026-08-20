package auth

import (
	"appointment-manager/internal/session"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubSessionStorer is an in-memory session.Storer, so the auth tests can
// exercise a real session.Store without a database.
type stubSessionStorer struct {
	sessions map[string]session.Session
}

func newStubSessionStorer() *stubSessionStorer {
	return &stubSessionStorer{sessions: make(map[string]session.Session)}
}

func (s *stubSessionStorer) Create(_ context.Context, value session.Session) error {
	s.sessions[value.ID] = value

	return nil
}

func (s *stubSessionStorer) Get(_ context.Context, id string) (*session.Session, error) {
	value, ok := s.sessions[id]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	copied := value

	return &copied, nil
}

func (s *stubSessionStorer) Delete(_ context.Context, id string) error {
	if _, ok := s.sessions[id]; !ok {
		return session.ErrSessionNotFound
	}
	delete(s.sessions, id)

	return nil
}

func (s *stubSessionStorer) DeleteByAssistant(_ context.Context, userID string) (int64, error) {
	removed := int64(0)
	for id, value := range s.sessions {
		if value.UserID == userID {
			delete(s.sessions, id)
			removed++
		}
	}

	return removed, nil
}

func (s *stubSessionStorer) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	removed := int64(0)
	for id, value := range s.sessions {
		if before.After(value.ExpiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}

	return removed, nil
}

func newTestSessionStore(t *testing.T) *session.Store {
	t.Helper()

	store, err := session.NewStore(newStubSessionStorer())
	require.NoError(t, err)

	return store
}
