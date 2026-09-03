//go:build integration

package outbox_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/outbox"
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	outboxIntegrationImage    = "postgres:18.3-alpine3.23"
	outboxIntegrationDBName   = "appointment_manager"
	outboxIntegrationDBUser   = "appointment_user"
	outboxIntegrationDBPass   = "appointment_password"
	outboxIntegrationSSLParam = "sslmode=disable"

	outboxIntegrationAggregate outbox.AggregateType = "test_aggregate"
	outboxIntegrationEvent     outbox.EventType     = "test.event"
)

func newOutboxIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		outboxIntegrationImage,
		postgres.WithDatabase(outboxIntegrationDBName),
		postgres.WithUsername(outboxIntegrationDBUser),
		postgres.WithPassword(outboxIntegrationDBPass),
		testcontainers.WithEnv(map[string]string{"PGDATA": "/var/lib/postgresql/data"}),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, outboxIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

type outboxRow struct {
	aggregateType string
	eventType     string
	lastError     *string
	payload       []byte
	aggregateID   uuid.UUID
	processedAt   *time.Time
	availableAt   time.Time
	createdAt     time.Time
	id            int64
	attempts      int16
}

func selectOutboxRowByAggregate(ctx context.Context, t *testing.T, pool *pgxpool.Pool, aggregateID uuid.UUID) outboxRow {
	t.Helper()

	var row outboxRow
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, payload,
		       attempts, created_at, available_at, processed_at, last_error
		FROM public.outbox
		WHERE aggregate_id = $1
	`, aggregateID).Scan(
		&row.id, &row.aggregateType, &row.aggregateID, &row.eventType, &row.payload,
		&row.attempts, &row.createdAt, &row.availableAt, &row.processedAt, &row.lastError,
	))

	return row
}

func TestInsertPersistsEvent(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)
	aggregateID := uuid.NewV7()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, outbox.Insert(ctx, tx, outbox.Event{
		AggregateType: outboxIntegrationAggregate,
		AggregateID:   aggregateID,
		EventType:     outboxIntegrationEvent,
		Payload:       map[string]string{"reason": "test"},
	}))
	require.NoError(t, tx.Commit(ctx))

	row := selectOutboxRowByAggregate(ctx, t, pool, aggregateID)
	require.Equal(t, string(outboxIntegrationAggregate), row.aggregateType)
	require.Equal(t, string(outboxIntegrationEvent), row.eventType)
	require.JSONEq(t, `{"reason":"test"}`, string(row.payload))
	require.Zero(t, row.attempts)
	require.Nil(t, row.processedAt)
	require.Nil(t, row.lastError)
}

func TestInsertNilPayload(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, outbox.Insert(ctx, tx, outbox.Event{
		AggregateType: outboxIntegrationAggregate,
		AggregateID:   uuid.NewV7(),
		EventType:     outboxIntegrationEvent,
	}))
	require.NoError(t, tx.Commit(ctx))

	var payload []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT payload FROM public.outbox ORDER BY id DESC LIMIT 1`).Scan(&payload))
	require.JSONEq(t, `{}`, string(payload))
}

func TestInsertValidation(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newOutboxIntegrationPool(ctx, t)

	tests := map[string]struct {
		event   outbox.Event
		wantErr error
	}{
		"empty aggregate type": {
			event:   outbox.Event{AggregateID: uuid.NewV7(), EventType: outboxIntegrationEvent},
			wantErr: outbox.ErrEmptyAggregateType,
		},
		"nil aggregate id": {
			event:   outbox.Event{AggregateType: outboxIntegrationAggregate, EventType: outboxIntegrationEvent},
			wantErr: outbox.ErrNilAggregateID,
		},
		"empty event type": {
			event:   outbox.Event{AggregateType: outboxIntegrationAggregate, AggregateID: uuid.NewV7()},
			wantErr: outbox.ErrEmptyEventType,
		},
		"payload not an object": {
			event: outbox.Event{
				AggregateType: outboxIntegrationAggregate,
				AggregateID:   uuid.NewV7(),
				EventType:     outboxIntegrationEvent,
				Payload:       []string{"a"},
			},
			wantErr: outbox.ErrPayloadNotObject,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()

			require.ErrorIs(t, outbox.Insert(ctx, tx, tt.event), tt.wantErr)
		})
	}
}
