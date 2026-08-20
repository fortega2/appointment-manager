package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	CookieName      = "appointment_manager_session"
	SessionDuration = 24 * time.Hour

	bytesPerSession = 32
)

// Session is an authenticated assistant. ID is the digest of the token the
// cookie carries, never the token itself.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Storer interface {
	Create(ctx context.Context, s Session) error
	Get(ctx context.Context, id string) (*Session, error)
	Delete(ctx context.Context, id string) error
	DeleteByAssistant(ctx context.Context, userID string) (int64, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// Store manages the session lifecycle on top of a Storer. It keeps no state of
// its own: every read goes to the storer.
type Store struct {
	storer Storer
}

func NewStore(storer Storer) (*Store, error) {
	if storer == nil {
		return nil, ErrNilStorer
	}

	return &Store{storer: storer}, nil
}

// Create persists a session for the assistant and returns the token to put in
// the cookie. Only the token's digest is stored.
func (s *Store) Create(ctx context.Context, userID string) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	now := time.Now()

	if err := s.storer.Create(ctx, Session{
		ID:        hashToken(token),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionDuration),
	}); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}

	return token, nil
}

// Get resolves a cookie token into its session. An expired session yields
// ErrSessionExpired and is left for DeleteExpired to remove.
func (s *Store) Get(ctx context.Context, token string) (*Session, error) {
	session, err := s.storer.Get(ctx, hashToken(token))
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("get: %w", ErrSessionExpired)
	}

	return session, nil
}

// Delete removes the session behind a cookie token. It returns
// ErrSessionNotFound if there was none.
func (s *Store) Delete(ctx context.Context, token string) error {
	if err := s.storer.Delete(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	return nil
}

// DeleteByAssistant removes every session the assistant holds and reports how
// many. Finding none is not an error: it means they were logged in nowhere.
func (s *Store) DeleteByAssistant(ctx context.Context, userID string) (int64, error) {
	removed, err := s.storer.DeleteByAssistant(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("delete by assistant: %w", err)
	}

	return removed, nil
}

// DeleteExpired removes every expired session and reports how many. Its
// signature is worker.JobFunc, so it can run as a periodic sweep.
func (s *Store) DeleteExpired(ctx context.Context) (int64, error) {
	removed, err := s.storer.DeleteExpired(ctx, time.Now())
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}

	return removed, nil
}

func generateToken() (string, error) {
	b := make([]byte, bytesPerSession)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	return base64.URLEncoding.EncodeToString(b), nil
}

// hashToken derives the stored session key from a cookie token. An unsalted
// SHA-256 is enough: the token is 256 bits of crypto/rand.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
