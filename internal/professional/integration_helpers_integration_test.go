//go:build integration

package professional_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/professional"
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
	professionalIntegrationImage    = "postgres:18.3-alpine3.23"
	professionalIntegrationDBName   = "appointment_manager"
	professionalIntegrationDBUser   = "appointment_user"
	professionalIntegrationDBPass   = "appointment_password"
	professionalIntegrationSSLParam = "sslmode=disable"
)

func newProfessionalIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		professionalIntegrationImage,
		postgres.WithDatabase(professionalIntegrationDBName),
		postgres.WithUsername(professionalIntegrationDBUser),
		postgres.WithPassword(professionalIntegrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, professionalIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newProfessionalIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *professional.Repository {
	t.Helper()

	repo, err := professional.NewRepository(pool)
	require.NoError(t, err)

	return repo
}

func newProfessionalIntegrationMux(t *testing.T, repo *professional.Repository) *http.ServeMux {
	t.Helper()

	h, err := professional.NewHandler(slog.New(slog.DiscardHandler), repo)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}
