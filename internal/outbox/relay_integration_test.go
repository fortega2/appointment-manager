//go:build integration

package outbox_test

import (
	"appointment-manager/internal/outbox"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const outboxIntegrationBatchSize = 10

var errRelayHandlerFailed = errors.New("handler failed")

// spyHandler records every call it receives and can be made to fail or to hold
// the row's transaction open for a while, widening the window in which two
// concurrent Drain calls could otherwise race for the same row.
type spyHandler struct {
	mu       sync.Mutex
	calls    []uuid.UUID
	delay    time.Duration
	failNext bool
}

func (s *spyHandler) handle(_ context.Context, aggregateID uuid.UUID, _ json.RawMessage) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, aggregateID)
	if s.failNext {
		return errRelayHandlerFailed
	}

	return nil
}

func (s *spyHandler) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.calls)
}

func insertOutboxEvent(ctx context.Context, t *testing.T, pool *pgxpool.Pool, eventType outbox.EventType) uuid.UUID {
	t.Helper()

	aggregateID := uuid.NewV7()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, outbox.Insert(ctx, tx, outbox.Event{
		AggregateType: outboxIntegrationAggregate,
		AggregateID:   aggregateID,
		EventType:     eventType,
	}))
	require.NoError(t, tx.Commit(ctx))

	return aggregateID
}

func newTestRelay(t *testing.T, pool *pgxpool.Pool, batchSize int) *outbox.Relay {
	t.Helper()

	relay, err := outbox.NewRelay(slog.New(slog.DiscardHandler), pool, batchSize)
	require.NoError(t, err)

	return relay
}

func TestRelayDrainDeliversAndMarksProcessed(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)
	aggregateID := insertOutboxEvent(ctx, t, pool, outboxIntegrationEvent)

	relay := newTestRelay(t, pool, outboxIntegrationBatchSize)
	handler := &spyHandler{}
	require.NoError(t, relay.Register(outboxIntegrationEvent, handler.handle))

	delivered, err := relay.Drain(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), delivered)
	require.Equal(t, []uuid.UUID{aggregateID}, handler.calls)

	row := selectOutboxRowByAggregate(ctx, t, pool, aggregateID)
	require.NotNil(t, row.processedAt)

	// A processed row is gone from the pending backlog, so a second drain must
	// not redeliver it.
	delivered, err = relay.Drain(ctx)
	require.NoError(t, err)
	require.Zero(t, delivered)
	require.Equal(t, 1, handler.callCount())
}

func TestRelayDrainHandlerFailureBacksOff(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)
	aggregateID := insertOutboxEvent(ctx, t, pool, outboxIntegrationEvent)

	relay := newTestRelay(t, pool, outboxIntegrationBatchSize)
	handler := &spyHandler{failNext: true}
	require.NoError(t, relay.Register(outboxIntegrationEvent, handler.handle))

	delivered, err := relay.Drain(ctx)
	require.NoError(t, err)
	require.Zero(t, delivered)
	require.Equal(t, 1, handler.callCount())

	row := selectOutboxRowByAggregate(ctx, t, pool, aggregateID)
	require.Nil(t, row.processedAt)
	require.EqualValues(t, 1, row.attempts)
	require.NotNil(t, row.lastError)
	require.Equal(t, errRelayHandlerFailed.Error(), *row.lastError)
	require.True(t, row.availableAt.After(time.Now()))

	// available_at was pushed into the future, so an immediate second drain must
	// not retry it yet.
	delivered, err = relay.Drain(ctx)
	require.NoError(t, err)
	require.Zero(t, delivered)
	require.Equal(t, 1, handler.callCount())
}

func TestRelayDrainNoHandlerRegistered(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)
	aggregateID := insertOutboxEvent(ctx, t, pool, "unregistered.event")

	relay := newTestRelay(t, pool, outboxIntegrationBatchSize)

	delivered, err := relay.Drain(ctx)
	require.NoError(t, err)
	require.Zero(t, delivered)

	row := selectOutboxRowByAggregate(ctx, t, pool, aggregateID)
	require.Nil(t, row.processedAt)
	require.EqualValues(t, 1, row.attempts)
	require.NotNil(t, row.lastError)
	require.Equal(t, outbox.ErrNoHandlerRegistered.Error(), *row.lastError)
}

func TestRelayDrainRespectsBatchSize(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)

	for range 3 {
		insertOutboxEvent(ctx, t, pool, outboxIntegrationEvent)
	}

	relay := newTestRelay(t, pool, 2)
	handler := &spyHandler{}
	require.NoError(t, relay.Register(outboxIntegrationEvent, handler.handle))

	delivered, err := relay.Drain(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), delivered)

	delivered, err = relay.Drain(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), delivered)

	require.Equal(t, 3, handler.callCount())
}

func TestRelayDrainSkipsEventsNotYetDue(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)
	aggregateID := insertOutboxEvent(ctx, t, pool, outboxIntegrationEvent)

	_, err := pool.Exec(ctx, `
		UPDATE public.outbox SET available_at = CURRENT_TIMESTAMP + INTERVAL '1 hour'
		WHERE aggregate_id = $1
	`, aggregateID)
	require.NoError(t, err)

	relay := newTestRelay(t, pool, outboxIntegrationBatchSize)
	handler := &spyHandler{}
	require.NoError(t, relay.Register(outboxIntegrationEvent, handler.handle))

	delivered, err := relay.Drain(ctx)
	require.NoError(t, err)
	require.Zero(t, delivered)
	require.Zero(t, handler.callCount())
}

// TestRelayDrainSkipLockedPreventsDoubleDelivery is what FOR UPDATE SKIP LOCKED
// exists for: two Relay instances draining the same table concurrently, as
// would happen if the process ran more than one replica, must split the
// backlog rather than both claiming the same row.
func TestRelayDrainSkipLockedPreventsDoubleDelivery(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)

	const eventCount = 6
	ids := make([]uuid.UUID, eventCount)
	for i := range ids {
		ids[i] = insertOutboxEvent(ctx, t, pool, outboxIntegrationEvent)
	}

	handler := &spyHandler{delay: 100 * time.Millisecond}
	relayA := newTestRelay(t, pool, eventCount/2)
	relayB := newTestRelay(t, pool, eventCount/2)
	require.NoError(t, relayA.Register(outboxIntegrationEvent, handler.handle))
	require.NoError(t, relayB.Register(outboxIntegrationEvent, handler.handle))

	var wg sync.WaitGroup
	var deliveredA, deliveredB int64
	var errA, errB error

	wg.Add(2)
	go func() { defer wg.Done(); deliveredA, errA = relayA.Drain(ctx) }()
	go func() { defer wg.Done(); deliveredB, errB = relayB.Drain(ctx) }()
	wg.Wait()

	require.NoError(t, errA)
	require.NoError(t, errB)
	require.Equal(t, int64(eventCount), deliveredA+deliveredB)

	seen := make(map[uuid.UUID]int, eventCount)
	for _, id := range handler.calls {
		seen[id]++
	}
	for _, id := range ids {
		require.Equal(t, 1, seen[id], "event delivered more than once")
	}
}
