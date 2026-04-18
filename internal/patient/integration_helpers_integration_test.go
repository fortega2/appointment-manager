//go:build integration

package patient_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/patient"
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	patientIntegrationImage    = "postgres:18.3-alpine3.23"
	patientIntegrationDBName   = "appointment_manager"
	patientIntegrationDBUser   = "appointment_user"
	patientIntegrationDBPass   = "appointment_password"
	patientIntegrationSSLParam = "sslmode=disable"
)

func newPatientIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		patientIntegrationImage,
		postgres.WithDatabase(patientIntegrationDBName),
		postgres.WithUsername(patientIntegrationDBUser),
		postgres.WithPassword(patientIntegrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, patientIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newPatientIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *patient.Repository {
	t.Helper()

	repo, err := patient.NewRepository(pool)
	require.NoError(t, err)

	return repo
}

func newPatientIntegrationMux(t *testing.T, repo *patient.Repository) *http.ServeMux {
	t.Helper()

	h, err := patient.NewHandler(slog.New(slog.DiscardHandler), repo)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func countPatients(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var total int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM patient`).Scan(&total)
	require.NoError(t, err)

	return total
}
