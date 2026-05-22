//go:build integration

package healthinsurance_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/healthinsurance"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	healthInsuranceIntegrationImage    = "postgres:18.3-alpine3.23"
	healthInsuranceIntegrationDBName   = "appointment_manager"
	healthInsuranceIntegrationDBUser   = "appointment_user"
	healthInsuranceIntegrationDBPass   = "appointment_password"
	healthInsuranceIntegrationSSLParam = "sslmode=disable"
)

func newHealthInsuranceIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		healthInsuranceIntegrationImage,
		postgres.WithDatabase(healthInsuranceIntegrationDBName),
		postgres.WithUsername(healthInsuranceIntegrationDBUser),
		postgres.WithPassword(healthInsuranceIntegrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, healthInsuranceIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newHealthInsuranceIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *healthinsurance.Repository {
	t.Helper()

	repo, err := healthinsurance.NewRepository(pool)
	require.NoError(t, err)

	return repo
}
