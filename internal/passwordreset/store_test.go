package passwordreset

import (
	"appointment-manager/internal/token"
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	storeTTL   = 30 * time.Minute
	pastOffset = -time.Hour
)

var errStorerFailed = errors.New("storer failed")

type stubStorer struct {
	tokens     map[string]Token
	createErr  error
	getErr     error
	consumeErr error
	expiredErr error
	lastBefore time.Time
}

func newStubStorer() *stubStorer {
	return &stubStorer{tokens: make(map[string]Token)}
}

// Create mirrors the repository's contract: issuing a token drops whatever the
// assistant already held, so the tests exercise the same replacement the CTE in
// createPasswordResetTokenQuery performs.
func (s *stubStorer) Create(_ context.Context, t Token) error {
	if s.createErr != nil {
		return s.createErr
	}
	for id, existing := range s.tokens {
		if existing.AssistantID == t.AssistantID {
			delete(s.tokens, id)
		}
	}
	s.tokens[t.ID] = t

	return nil
}

func (s *stubStorer) Get(_ context.Context, id string) (*Token, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	t, ok := s.tokens[id]
	if !ok {
		return nil, ErrTokenNotFound
	}
	copied := t

	return &copied, nil
}

func (s *stubStorer) Consume(_ context.Context, id string) (*Token, error) {
	if s.consumeErr != nil {
		return nil, s.consumeErr
	}
	t, ok := s.tokens[id]
	if !ok {
		return nil, ErrTokenNotFound
	}
	delete(s.tokens, id)
	copied := t

	return &copied, nil
}

func (s *stubStorer) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	if s.expiredErr != nil {
		return 0, s.expiredErr
	}
	s.lastBefore = before

	removed := int64(0)
	for id, t := range s.tokens {
		if before.After(t.ExpiresAt) {
			delete(s.tokens, id)
			removed++
		}
	}

	return removed, nil
}

func newTestStore(t *testing.T, storer Storer) *Store {
	t.Helper()

	store, err := NewStore(storer, storeTTL)
	require.NoError(t, err)

	return store
}

func TestNewStoreValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		storer   Storer
		ttl      time.Duration
		expected []error
	}{
		{
			name:     "nil storer",
			storer:   nil,
			ttl:      storeTTL,
			expected: []error{ErrNilStorer},
		},
		{
			name:     "zero ttl",
			storer:   newStubStorer(),
			ttl:      0,
			expected: []error{ErrNonPositiveTTL},
		},
		{
			name:     "negative ttl",
			storer:   newStubStorer(),
			ttl:      pastOffset,
			expected: []error{ErrNonPositiveTTL},
		},
		{
			name:     "every problem at once",
			storer:   nil,
			ttl:      0,
			expected: []error{ErrNilStorer, ErrNonPositiveTTL},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, err := NewStore(tt.storer, tt.ttl)
			require.Error(t, err)
			assert.Nil(t, store)

			for _, expected := range tt.expected {
				assert.ErrorIs(t, err, expected)
			}
		})
	}
}

func TestStoreCreateAndVerify(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())
	assistantID := uuid.NewV7()

	token, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	require.NoError(t, store.Verify(t.Context(), token))
}

func TestStoreCreatePersistsDigestNotToken(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	store := newTestStore(t, storer)

	tken, err := store.Create(t.Context(), uuid.NewV7())
	require.NoError(t, err)

	require.Len(t, storer.tokens, 1)
	_, storedUnderToken := storer.tokens[tken]
	assert.False(t, storedUnderToken, "the token carried by the link must never be the stored key")

	stored, ok := storer.tokens[token.Hash(tken)]
	require.True(t, ok)
	assert.Len(t, stored.ID, 64)
	assert.NotEqual(t, tken, stored.ID)
	assert.True(t, stored.ExpiresAt.After(stored.CreatedAt))
}

func TestStoreCreateIssuesADistinctTokenEachTime(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())
	assistantID := uuid.NewV7()

	first, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)
	second, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

// TestStoreCreateVoidsThePreviousLink pins the property the CTE in the
// repository exists for: asking for a second link must leave the first one dead.
func TestStoreCreateVoidsThePreviousLink(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	store := newTestStore(t, storer)
	assistantID := uuid.NewV7()

	first, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)

	second, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)

	assert.Len(t, storer.tokens, 1)
	require.NoError(t, store.Verify(t.Context(), second))

	err = store.Verify(t.Context(), first)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestStoreCreateNilAssistantID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	token, err := store.Create(t.Context(), uuid.Nil())
	require.Error(t, err)
	assert.Empty(t, token)
	assert.ErrorIs(t, err, ErrNilAssistantID)
}

func TestStoreCreateStorerError(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.createErr = errStorerFailed
	store := newTestStore(t, storer)

	token, err := store.Create(t.Context(), uuid.NewV7())
	require.Error(t, err)
	assert.Empty(t, token)
	assert.ErrorIs(t, err, errStorerFailed)
}

func TestStoreVerifyNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	err := store.Verify(t.Context(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestStoreVerifyTamperedToken(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	token, err := store.Create(t.Context(), uuid.NewV7())
	require.NoError(t, err)

	err = store.Verify(t.Context(), token+"tampered")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestStoreVerifyExpired(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.tokens[token.Hash("expired")] = Token{
		ID:          token.Hash("expired"),
		AssistantID: uuid.NewV7(),
		CreatedAt:   time.Now().Add(2 * pastOffset),
		ExpiresAt:   time.Now().Add(pastOffset),
	}
	store := newTestStore(t, storer)

	err := store.Verify(t.Context(), "expired")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

// TestStoreVerifyDoesNotSpendTheToken is the whole reason Verify exists next to
// Consume: the page behind the link renders before the password is chosen, so
// checking it must leave it redeemable.
func TestStoreVerifyDoesNotSpendTheToken(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	store := newTestStore(t, storer)
	assistantID := uuid.NewV7()

	token, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)

	require.NoError(t, store.Verify(t.Context(), token))
	require.NoError(t, store.Verify(t.Context(), token))
	assert.Len(t, storer.tokens, 1)

	consumed, err := store.Consume(t.Context(), token)
	require.NoError(t, err)
	assert.Equal(t, assistantID, consumed)
}

func TestStoreConsume(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())
	assistantID := uuid.NewV7()

	token, err := store.Create(t.Context(), assistantID)
	require.NoError(t, err)

	consumed, err := store.Consume(t.Context(), token)
	require.NoError(t, err)
	assert.Equal(t, assistantID, consumed)
}

// TestStoreConsumeIsSingleUse is the property that makes a leaked link harmless
// once it has been redeemed.
func TestStoreConsumeIsSingleUse(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	store := newTestStore(t, storer)

	token, err := store.Create(t.Context(), uuid.NewV7())
	require.NoError(t, err)

	_, err = store.Consume(t.Context(), token)
	require.NoError(t, err)
	assert.Empty(t, storer.tokens)

	consumed, err := store.Consume(t.Context(), token)
	require.Error(t, err)
	assert.Equal(t, uuid.Nil(), consumed)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

// TestStoreConsumeExpiredSpendsTheTokenAnyway pins the deliberate ordering in
// Consume: the delete happens first, so an expired link is reported as expired
// and is also gone, rather than lingering for someone to retry.
func TestStoreConsumeExpiredSpendsTheTokenAnyway(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.tokens[token.Hash("expired")] = Token{
		ID:          token.Hash("expired"),
		AssistantID: uuid.NewV7(),
		CreatedAt:   time.Now().Add(2 * pastOffset),
		ExpiresAt:   time.Now().Add(pastOffset),
	}
	store := newTestStore(t, storer)

	consumed, err := store.Consume(t.Context(), "expired")
	require.Error(t, err)
	assert.Equal(t, uuid.Nil(), consumed)
	assert.ErrorIs(t, err, ErrTokenExpired)
	assert.Empty(t, storer.tokens)
}

func TestStoreConsumeNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, newStubStorer())

	consumed, err := store.Consume(t.Context(), "missing")
	require.Error(t, err)
	assert.Equal(t, uuid.Nil(), consumed)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestStoreConsumeStorerError(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.consumeErr = errStorerFailed
	store := newTestStore(t, storer)

	consumed, err := store.Consume(t.Context(), "any")
	require.Error(t, err)
	assert.Equal(t, uuid.Nil(), consumed)
	assert.ErrorIs(t, err, errStorerFailed)
}

func TestStoreDeleteExpired(t *testing.T) {
	t.Parallel()

	storer := newStubStorer()
	storer.tokens["expired"] = Token{
		ID:        "expired",
		ExpiresAt: time.Now().Add(pastOffset),
	}
	storer.tokens["active"] = Token{
		ID:        "active",
		ExpiresAt: time.Now().Add(storeTTL),
	}
	store := newTestStore(t, storer)

	removed, err := store.DeleteExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
	assert.False(t, storer.lastBefore.IsZero())

	_, expiredFound := storer.tokens["expired"]
	assert.False(t, expiredFound)
	_, activeFound := storer.tokens["active"]
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
