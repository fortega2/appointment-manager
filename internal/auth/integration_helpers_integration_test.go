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
	"time"

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

// stubSessionStorer is an in-memory session.Storer. The integration tests here
// cover the auth handlers, not session persistence, which has its own suite.
type stubSessionStorer struct {
	sessions map[string]session.Session
}

func (s *stubSessionStorer) Create(_ context.Context, value session.Session) error {
	s.sessions[value.ID] = value

	return nil
}

func (s *stubSessionStorer) Get(_ context.Context, id string) (*session.Session, error) {
	value, ok := s.sessions[id]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	copied := value

	return &copied, nil
}

func (s *stubSessionStorer) Delete(_ context.Context, id string) error {
	if _, ok := s.sessions[id]; !ok {
		return session.ErrSessionNotFound
	}
	delete(s.sessions, id)

	return nil
}

func (s *stubSessionStorer) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	removed := int64(0)
	for id, value := range s.sessions {
		if before.After(value.ExpiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}

	return removed, nil
}

func newTestSessionStore(t *testing.T) *session.Store {
	t.Helper()

	store, err := session.NewStore(&stubSessionStorer{sessions: make(map[string]session.Session)})
	require.NoError(t, err)

	return store
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
