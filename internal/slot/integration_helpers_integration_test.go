//go:build integration

package slot_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/slot"
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	integrationImage    = "postgres:18.3-alpine3.23"
	integrationDBName   = "appointment_manager"
	integrationDBUser   = "appointment_user"
	integrationDBPass   = "appointment_password"
	integrationSSLParam = "sslmode=disable"

	integrationProfessionalFirstName = "Laura"
	integrationProfessionalLastName  = "Gomez"
	integrationProfessionalPhone     = "1133334444"

	integrationCancelMaxCapacity = int16(5)
	integrationCancelStartTime   = "2026-05-25T15:00:00Z"
	integrationCancelEndTime     = "2026-05-25T16:00:00Z"
	integrationSlotsPath         = "/slots/"
)

var integrationDate = time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)

func newSlotIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		integrationImage,
		postgres.WithDatabase(integrationDBName),
		postgres.WithUsername(integrationDBUser),
		postgres.WithPassword(integrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, integrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newSlotIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *slot.Repository {
	t.Helper()

	repo, err := slot.NewRepository(pool)
	require.NoError(t, err)

	return repo
}

// newSlotIntegrationMux wires a slot handler backed by the real pool, with the
// two cross-package collaborators replaced by calls, so the tests observe what
// the handler asked for without depending on the appointment package.
func newSlotIntegrationMux(t *testing.T, pool *pgxpool.Pool, calls *recordedCalls) *http.ServeMux {
	t.Helper()

	query, err := slot.NewQuery(pool)
	require.NoError(t, err)

	pRepo, err := professional.NewRepository(pool)
	require.NoError(t, err)

	h, err := slot.NewHandler(
		slog.New(slog.DiscardHandler),
		newSlotIntegrationRepository(t, pool),
		query,
		pRepo,
		calls.cancelAppointments,
		calls.sendNotification,
	)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterUIHandlers(mux)

	return mux
}

func seedCancellableSlot(ctx context.Context, t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	professionalID := uuid.Must(uuid.NewV7())
	insertProfessionalForSlot(ctx, t, pool, professionalID)

	newRecord, err := slot.NewSlot(
		professionalID,
		integrationDate,
		mustParseTime(integrationCancelStartTime),
		mustParseTime(integrationCancelEndTime),
		integrationCancelMaxCapacity,
	)
	require.NoError(t, err)
	require.NoError(t, newSlotIntegrationRepository(t, pool).Create(ctx, newRecord))

	return newRecord.ID
}

func insertProfessionalForSlot(ctx context.Context, t *testing.T, pool *pgxpool.Pool, professionalID uuid.UUID) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO professional (id, first_name, last_name, phone, specialty, active)
		VALUES ($1, $2, $3, $4, 'kinesiology', true)
	`, professionalID, integrationProfessionalFirstName, integrationProfessionalLastName, integrationProfessionalPhone)
	require.NoError(t, err)
}

func fetchSlotRecord(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID) slot.Slot {
	t.Helper()

	var persisted slot.Slot
	err := pool.QueryRow(ctx, `
		SELECT
			id,
			professional_id,
			date,
			start_time,
			end_time,
			max_capacity,
			blocked
		FROM
			public.slot
		WHERE
			id = $1
	`, id).Scan(
		&persisted.ID,
		&persisted.ProfessionalID,
		&persisted.Date,
		&persisted.StartTime,
		&persisted.EndTime,
		&persisted.MaxCapacity,
		&persisted.Blocked,
	)
	require.NoError(t, err)

	return persisted
}
