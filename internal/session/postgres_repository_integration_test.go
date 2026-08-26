//go:build integration

package session_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/session"
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	sessionIntegrationImage    = "postgres:18.3-alpine3.23"
	sessionIntegrationDBName   = "appointment_manager"
	sessionIntegrationDBUser   = "appointment_user"
	sessionIntegrationDBPass   = "appointment_password"
	sessionIntegrationSSLParam = "sslmode=disable"

	sessionIntegrationEmail      = "assistant@email.com"
	sessionIntegrationOtherEmail = "other@email.com"
	sessionIntegrationHash       = "0000000000000000000000000000000000000000000000000000000000000000"
	sessionIntegrationSecondHash = "1111111111111111111111111111111111111111111111111111111111111111"
	sessionIntegrationThirdHash  = "2222222222222222222222222222222222222222222222222222222222222222"
)

func newSessionIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		sessionIntegrationImage,
		postgres.WithDatabase(sessionIntegrationDBName),
		postgres.WithUsername(sessionIntegrationDBUser),
		postgres.WithPassword(sessionIntegrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, sessionIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newSessionIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *session.PostgresRepository {
	t.Helper()

	repo, err := session.NewPostgresRepository(pool)
	require.NoError(t, err)

	return repo
}

func seedAssistant(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	id := uuid.NewV7()
	_, err := pool.Exec(ctx, `
		INSERT INTO assistant (id, first_name, last_name, email, password_hash)
		VALUES ($1, $2, $3, $4, $5)
	`, id, "Ana", "Gomez", email, "hash")
	require.NoError(t, err)

	return id
}

func countSessions(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var total int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.session`).Scan(&total))

	return total
}

func TestPostgresRepositoryCreateAndGet(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)
	now := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.Create(ctx, session.Session{
		ID:        sessionIntegrationHash,
		UserID:    assistantID.String(),
		CreatedAt: now,
		ExpiresAt: now.Add(session.SessionDuration),
	}))

	stored, err := repo.Get(ctx, sessionIntegrationHash)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, sessionIntegrationHash, stored.ID)
	assert.Equal(t, assistantID.String(), stored.UserID)
	assert.WithinDuration(t, now, stored.CreatedAt, time.Millisecond)
	assert.WithinDuration(t, now.Add(session.SessionDuration), stored.ExpiresAt, time.Millisecond)
}

func TestPostgresRepositoryGetNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	stored, err := repo.Get(ctx, sessionIntegrationHash)
	require.Error(t, err)
	assert.Nil(t, stored)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestPostgresRepositoryCreateUnknownAssistant(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	now := time.Now().UTC()
	err := repo.Create(ctx, session.Session{
		ID:        sessionIntegrationHash,
		UserID:    uuid.NewV7().String(),
		CreatedAt: now,
		ExpiresAt: now.Add(session.SessionDuration),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, session.ErrUnknownAssistant)
}

func TestPostgresRepositoryDelete(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)
	now := time.Now().UTC()
	require.NoError(t, repo.Create(ctx, session.Session{
		ID:        sessionIntegrationHash,
		UserID:    assistantID.String(),
		CreatedAt: now,
		ExpiresAt: now.Add(session.SessionDuration),
	}))

	require.NoError(t, repo.Delete(ctx, sessionIntegrationHash))
	assert.Zero(t, countSessions(ctx, t, pool))

	err := repo.Delete(ctx, sessionIntegrationHash)
	require.Error(t, err)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestPostgresRepositoryDeleteExpired(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)
	now := time.Now().UTC()

	require.NoError(t, repo.Create(ctx, session.Session{
		ID:        sessionIntegrationHash,
		UserID:    assistantID.String(),
		CreatedAt: now.Add(-2 * session.SessionDuration),
		ExpiresAt: now.Add(-time.Hour),
	}))
	activeID := sessionIntegrationSecondHash
	require.NoError(t, repo.Create(ctx, session.Session{
		ID:        activeID,
		UserID:    assistantID.String(),
		CreatedAt: now,
		ExpiresAt: now.Add(session.SessionDuration),
	}))

	removed, err := repo.DeleteExpired(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	active, err := repo.Get(ctx, activeID)
	require.NoError(t, err)
	assert.Equal(t, activeID, active.ID)
}

// TestPostgresRepositoryDeleteByAssistant is what makes a password reset log the
// assistant out everywhere, so it has to leave other assistants alone.
func TestPostgresRepositoryDeleteByAssistant(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)
	otherID := seedAssistant(ctx, t, pool, sessionIntegrationOtherEmail)
	now := time.Now().UTC()

	for _, id := range []string{sessionIntegrationHash, sessionIntegrationSecondHash} {
		require.NoError(t, repo.Create(ctx, session.Session{
			ID:        id,
			UserID:    assistantID.String(),
			CreatedAt: now,
			ExpiresAt: now.Add(session.SessionDuration),
		}))
	}
	require.NoError(t, repo.Create(ctx, session.Session{
		ID:        sessionIntegrationThirdHash,
		UserID:    otherID.String(),
		CreatedAt: now,
		ExpiresAt: now.Add(session.SessionDuration),
	}))

	removed, err := repo.DeleteByAssistant(ctx, assistantID.String())
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)
	assert.Equal(t, int64(1), countSessions(ctx, t, pool))

	survivor, err := repo.Get(ctx, sessionIntegrationThirdHash)
	require.NoError(t, err)
	assert.Equal(t, otherID.String(), survivor.UserID)
}

// TestPostgresRepositoryDeleteByAssistantWithoutSessions pins that an assistant
// logged in nowhere is a clean result, not ErrSessionNotFound.
func TestPostgresRepositoryDeleteByAssistantWithoutSessions(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)

	removed, err := repo.DeleteByAssistant(ctx, assistantID.String())
	require.NoError(t, err)
	assert.Zero(t, removed)
}

// TestPostgresRepositoryDeleteByAssistantUnknownAssistant covers the id that
// parses but matches nothing: a DELETE has no foreign key to violate.
func TestPostgresRepositoryDeleteByAssistantUnknownAssistant(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	removed, err := repo.DeleteByAssistant(ctx, uuid.NewV7().String())
	require.NoError(t, err)
	assert.Zero(t, removed)
}

// TestSessionSurvivesStoreRestart is the reason this package talks to Postgres
// at all: a cookie issued before a restart must still resolve after it. Store
// holds no state, so a fresh Store over a fresh repository is a faithful stand-in
// for a new process.
func TestSessionSurvivesStoreRestart(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)

	before, err := session.NewStore(newSessionIntegrationRepository(t, pool))
	require.NoError(t, err)

	token, err := before.Create(ctx, assistantID.String())
	require.NoError(t, err)
	require.NotEmpty(t, token)

	after, err := session.NewStore(newSessionIntegrationRepository(t, pool))
	require.NoError(t, err)

	restored, err := after.Get(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, restored)
	assert.Equal(t, assistantID.String(), restored.UserID)

	var storedID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM public.session`).Scan(&storedID))
	assert.NotEqual(t, token, storedID, "the cookie token must never be stored verbatim")

	require.NoError(t, after.Delete(ctx, token))
	_, err = after.Get(ctx, token)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestPostgresRepositoryDeletingAssistantCascades(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newSessionIntegrationPool(ctx, t)
	repo := newSessionIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, sessionIntegrationEmail)
	now := time.Now().UTC()
	require.NoError(t, repo.Create(ctx, session.Session{
		ID:        sessionIntegrationHash,
		UserID:    assistantID.String(),
		CreatedAt: now,
		ExpiresAt: now.Add(session.SessionDuration),
	}))

	_, err := pool.Exec(ctx, `DELETE FROM assistant WHERE id = $1`, assistantID)
	require.NoError(t, err)

	assert.Zero(t, countSessions(ctx, t, pool))
}
