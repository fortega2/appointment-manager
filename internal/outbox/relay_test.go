package outbox_test

import (
	"appointment-manager/internal/outbox"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// lazyPool builds a pool that never dials: pgxpool.New only connects on first
// use, so this is enough to exercise constructor validation without a database.
func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db")
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func noopHandler(context.Context, uuid.UUID, json.RawMessage) error { return nil }

func TestNewRelay(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	pool := lazyPool(t)

	t.Run("valid", func(t *testing.T) {
		relay, err := outbox.NewRelay(logger, pool, 10)
		require.NoError(t, err)
		require.NotNil(t, relay)
	})

	t.Run("nil logger", func(t *testing.T) {
		_, err := outbox.NewRelay(nil, pool, 10)
		require.ErrorIs(t, err, outbox.ErrNilLogger)
	})

	t.Run("nil pool", func(t *testing.T) {
		_, err := outbox.NewRelay(logger, nil, 10)
		require.ErrorIs(t, err, outbox.ErrNilPool)
	})

	t.Run("zero batch size", func(t *testing.T) {
		_, err := outbox.NewRelay(logger, pool, 0)
		require.ErrorIs(t, err, outbox.ErrInvalidBatchSize)
	})

	t.Run("negative batch size", func(t *testing.T) {
		_, err := outbox.NewRelay(logger, pool, -1)
		require.ErrorIs(t, err, outbox.ErrInvalidBatchSize)
	})
}

func TestRelayRegister(t *testing.T) {
	newRelay := func(t *testing.T) *outbox.Relay {
		t.Helper()
		relay, err := outbox.NewRelay(slog.New(slog.DiscardHandler), lazyPool(t), 10)
		require.NoError(t, err)
		return relay
	}

	t.Run("valid", func(t *testing.T) {
		require.NoError(t, newRelay(t).Register("test.event", noopHandler))
	})

	t.Run("empty event type", func(t *testing.T) {
		err := newRelay(t).Register("", noopHandler)
		require.ErrorIs(t, err, outbox.ErrEmptyEventType)
	})

	t.Run("nil handler", func(t *testing.T) {
		err := newRelay(t).Register("test.event", nil)
		require.ErrorIs(t, err, outbox.ErrNilHandler)
	})

	t.Run("duplicate event type", func(t *testing.T) {
		relay := newRelay(t)
		require.NoError(t, relay.Register("test.event", noopHandler))

		err := relay.Register("test.event", noopHandler)
		require.ErrorIs(t, err, outbox.ErrDuplicateHandler)
	})
}
