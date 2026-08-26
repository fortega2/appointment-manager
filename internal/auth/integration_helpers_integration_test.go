//go:build integration

package auth_test

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/auth"
	"appointment-manager/internal/db"
	"appointment-manager/internal/password"
	"appointment-manager/internal/ratelimit"
	"appointment-manager/internal/session"
	"context"
	"log/slog"
	"net/http"
	"sync"
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

	authRoomyBurst        = 1000
	authLimiterRefill     = time.Minute
	authLimiterMaxEntries = 128
)

func newAuthIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		authIntegrationImage,
		postgres.WithDatabase(authIntegrationDBName),
		postgres.WithUsername(authIntegrationDBUser),
		postgres.WithPassword(authIntegrationDBPass),
		testcontainers.WithEnv(map[string]string{"PGDATA": "/var/lib/postgresql/data"}),
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
//
// The mutex is load-bearing: TestLoginQueuesConcurrentPasswordChecks drives ten
// logins at once and every one of them reaches Create, so an unguarded map here
// is a data race in the test rather than in the code under test.
type stubSessionStorer struct {
	mu       sync.Mutex
	sessions map[string]session.Session
}

func (s *stubSessionStorer) Create(_ context.Context, value session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[value.ID] = value

	return nil
}

func (s *stubSessionStorer) Get(_ context.Context, id string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.sessions[id]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	copied := value

	return &copied, nil
}

func (s *stubSessionStorer) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[id]; !ok {
		return session.ErrSessionNotFound
	}
	delete(s.sessions, id)

	return nil
}

func (s *stubSessionStorer) DeleteByAssistant(_ context.Context, userID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := int64(0)
	for id, value := range s.sessions {
		if value.UserID == userID {
			delete(s.sessions, id)
			removed++
		}
	}

	return removed, nil
}

func (s *stubSessionStorer) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// newAuthIntegrationLimiter builds a limiter roomy enough that no test trips it
// by accident; tests about the limit itself ask for one sized to trip.
func newAuthIntegrationLimiter(t *testing.T, cfg ratelimit.Config) *ratelimit.Limiter {
	t.Helper()

	limiter, err := ratelimit.New(cfg, nil)
	require.NoError(t, err)

	return limiter
}

func authRoomyLimiterConfig() ratelimit.Config {
	return ratelimit.Config{
		Enabled:       true,
		AccountBurst:  authRoomyBurst,
		AccountRefill: authLimiterRefill,
		IPBurst:       authRoomyBurst,
		IPRefill:      authLimiterRefill,
		MaxEntries:    authLimiterMaxEntries,
	}
}

// authDisabledLimiterConfig is for the tests that drive concurrent logins to
// exercise something else -- the Argon2 queue -- and would otherwise be refused
// by the limit before ever reaching it.
func authDisabledLimiterConfig() ratelimit.Config {
	cfg := authRoomyLimiterConfig()
	cfg.Enabled = false

	return cfg
}

func newAuthIntegrationMux(
	t *testing.T,
	repo *assistant.PostgresRepository,
	store *session.Store,
	cfg ratelimit.Config,
	isDev bool,
) *http.ServeMux {
	t.Helper()

	limiter := newAuthIntegrationLimiter(t, cfg)

	h, err := auth.NewHandler(slog.New(slog.DiscardHandler), store, repo, password.NewArgon2(nil, password.WithTestCost()), limiter, isDev)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}
