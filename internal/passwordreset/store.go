// Package passwordreset issues and redeems the single-use tokens that back a
// password reset link. It mirrors internal/session: the caller receives a
// token, the store keeps only its digest, and expiry is absolute.
package passwordreset

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const bytesPerToken = 32

// Token is one outstanding reset link. ID is the digest of the token the link
// carries, never the token itself.
type Token struct {
	ID          string
	AssistantID uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Storer persists reset tokens. Create replaces whatever the assistant already
// had, and Consume deletes the row it returns.
//
// It is exported rather than kept private because the implementation in this
// package is not the only one: internal/auth implements it in its tests to
// drive a real Store without a database, the same way it already does with
// session.Storer.
type Storer interface {
	Create(ctx context.Context, t Token) error
	Get(ctx context.Context, id string) (*Token, error)
	Consume(ctx context.Context, id string) (*Token, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// Store manages the reset token lifecycle on top of a Storer. It keeps no state
// of its own: every read goes to the storer.
type Store struct {
	storer Storer
	ttl    time.Duration
}

func NewStore(storer Storer, ttl time.Duration) (*Store, error) {
	errs := make([]error, 0)

	if storer == nil {
		errs = append(errs, ErrNilStorer)
	}
	if ttl <= 0 {
		errs = append(errs, ErrNonPositiveTTL)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("new store: %w", errors.Join(errs...))
	}

	return &Store{storer: storer, ttl: ttl}, nil
}

// TTL is how long a token issued by Create stays redeemable.
func (s *Store) TTL() time.Duration {
	return s.ttl
}

// Create issues a reset token for the assistant and returns the token to put in
// the link. Only the token's digest is stored. Any token the assistant already
// held is invalidated, so asking for a second link voids the first.
func (s *Store) Create(ctx context.Context, assistantID uuid.UUID) (string, error) {
	if assistantID == uuid.Nil {
		return "", fmt.Errorf("create: %w", ErrNilAssistantID)
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	now := time.Now()

	if err := s.storer.Create(ctx, Token{
		ID:          hashToken(token),
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.ttl),
	}); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}

	return token, nil
}

// Verify reports whether a link's token is still redeemable without spending
// it, so the page behind the link can say "this link expired" before asking for
// a password rather than after.
func (s *Store) Verify(ctx context.Context, token string) error {
	stored, err := s.storer.Get(ctx, hashToken(token))
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	if time.Now().After(stored.ExpiresAt) {
		return fmt.Errorf("verify: %w", ErrTokenExpired)
	}

	return nil
}

// Consume redeems a link's token and reports whose password it authorises to
// change. Redeeming is a delete, so the token is spent even when it turns out
// to have expired and two racing requests cannot both succeed.
func (s *Store) Consume(ctx context.Context, token string) (uuid.UUID, error) {
	consumed, err := s.storer.Consume(ctx, hashToken(token))
	if err != nil {
		return uuid.Nil, fmt.Errorf("consume: %w", err)
	}

	if time.Now().After(consumed.ExpiresAt) {
		return uuid.Nil, fmt.Errorf("consume: %w", ErrTokenExpired)
	}

	return consumed.AssistantID, nil
}

// DeleteExpired removes every expired token and reports how many. Its signature
// is worker.JobFunc, so it can run as a periodic sweep.
func (s *Store) DeleteExpired(ctx context.Context) (int64, error) {
	removed, err := s.storer.DeleteExpired(ctx, time.Now())
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}

	return removed, nil
}

func generateToken() (string, error) {
	b := make([]byte, bytesPerToken)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	return base64.URLEncoding.EncodeToString(b), nil
}

// hashToken derives the stored key from a link's token. An unsalted SHA-256 is
// enough: the token is 256 bits of crypto/rand.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
