//go:build integration

package auth_test

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/auth"
	"appointment-manager/internal/db"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
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
	authIntegrationImage    = "postgres:18.3-alpine3.23"
	authIntegrationDBName   = "appointment_manager"
	authIntegrationDBUser   = "appointment_user"
	authIntegrationDBPass   = "appointment_password"
	authIntegrationSSLParam = "sslmode=disable"
)

func newAuthIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		authIntegrationImage,
		postgres.WithDatabase(authIntegrationDBName),
		postgres.WithUsername(authIntegrationDBUser),
		postgres.WithPassword(authIntegrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, authIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newAuthIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *assistant.PostgresRepository {
	t.Helper()

	repo, err := assistant.NewPostgresRepository(pool)
	require.NoError(t, err)

	return repo
}

func newAuthIntegrationMux(
	t *testing.T,
	repo *assistant.PostgresRepository,
	store *session.Store,
	isDev bool,
) *http.ServeMux {
	t.Helper()

	h, err := auth.NewHandler(slog.New(slog.DiscardHandler), store, repo, password.NewArgon2(), isDev)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}
