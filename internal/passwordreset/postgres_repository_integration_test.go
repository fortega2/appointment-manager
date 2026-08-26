//go:build integration

package passwordreset_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/passwordreset"
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
	integrationImage    = "postgres:18.3-alpine3.23"
	integrationDBName   = "appointment_manager"
	integrationDBUser   = "appointment_user"
	integrationDBPass   = "appointment_password"
	integrationSSLParam = "sslmode=disable"

	integrationEmail  = "assistant@email.com"
	integrationDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	integrationOther  = "1111111111111111111111111111111111111111111111111111111111111111"

	integrationTTL = 30 * time.Minute
)

func newIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		integrationImage,
		postgres.WithDatabase(integrationDBName),
		postgres.WithUsername(integrationDBUser),
		postgres.WithPassword(integrationDBPass),
		testcontainers.WithEnv(map[string]string{"PGDATA": "/var/lib/postgresql/data"}),
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

func newIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *passwordreset.PostgresRepository {
	t.Helper()

	repo, err := passwordreset.NewPostgresRepository(pool)
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

func countTokens(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var total int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.password_reset_token`).Scan(&total))

	return total
}

func TestPostgresRepositoryCreateAndGet(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, integrationEmail)
	now := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	stored, err := repo.Get(ctx, integrationDigest)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, integrationDigest, stored.ID)
	assert.Equal(t, assistantID, stored.AssistantID)
	assert.WithinDuration(t, now, stored.CreatedAt, time.Millisecond)
	assert.WithinDuration(t, now.Add(integrationTTL), stored.ExpiresAt, time.Millisecond)
}

func TestPostgresRepositoryGetNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	stored, err := repo.Get(ctx, integrationDigest)
	require.Error(t, err)
	assert.Nil(t, stored)
	assert.ErrorIs(t, err, passwordreset.ErrTokenNotFound)
}

func TestPostgresRepositoryCreateUnknownAssistant(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	now := time.Now().UTC()
	err := repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: uuid.NewV7(),
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, passwordreset.ErrUnknownAssistant)
}

// TestPostgresRepositoryCreateReplacesTheAssistantsToken is why the create query
// is a data-modifying CTE rather than a plain INSERT. The stub in the unit tests
// can only assert the contract; this asserts Postgres actually honours it.
func TestPostgresRepositoryCreateReplacesTheAssistantsToken(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, integrationEmail)
	now := time.Now().UTC()

	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))
	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationOther,
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	assert.Equal(t, int64(1), countTokens(ctx, t, pool))

	_, err := repo.Get(ctx, integrationDigest)
	require.Error(t, err)
	assert.ErrorIs(t, err, passwordreset.ErrTokenNotFound)

	surviving, err := repo.Get(ctx, integrationOther)
	require.NoError(t, err)
	assert.Equal(t, integrationOther, surviving.ID)
}

// TestPostgresRepositoryCreateLeavesOtherAssistantsAlone guards the upsert's
// conflict target: replacing one assistant's link must not touch anyone else's.
func TestPostgresRepositoryCreateLeavesOtherAssistantsAlone(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	first := seedAssistant(ctx, t, pool, integrationEmail)
	second := seedAssistant(ctx, t, pool, "other@email.com")
	now := time.Now().UTC()

	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: first,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))
	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationOther,
		AssistantID: second,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	assert.Equal(t, int64(2), countTokens(ctx, t, pool))
}

func TestPostgresRepositoryConsume(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, integrationEmail)
	now := time.Now().UTC()
	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	consumed, err := repo.Consume(ctx, integrationDigest)
	require.NoError(t, err)
	require.NotNil(t, consumed)
	assert.Equal(t, assistantID, consumed.AssistantID)
	assert.Zero(t, countTokens(ctx, t, pool))

	again, err := repo.Consume(ctx, integrationDigest)
	require.Error(t, err)
	assert.Nil(t, again)
	assert.ErrorIs(t, err, passwordreset.ErrTokenNotFound)
}

func TestPostgresRepositoryDeleteExpired(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	expiredOwner := seedAssistant(ctx, t, pool, integrationEmail)
	activeOwner := seedAssistant(ctx, t, pool, "other@email.com")
	now := time.Now().UTC()

	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: expiredOwner,
		CreatedAt:   now.Add(-2 * integrationTTL),
		ExpiresAt:   now.Add(-time.Hour),
	}))
	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationOther,
		AssistantID: activeOwner,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	removed, err := repo.DeleteExpired(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	active, err := repo.Get(ctx, integrationOther)
	require.NoError(t, err)
	assert.Equal(t, integrationOther, active.ID)
}

// TestPostgresRepositoryDeleteByAssistant covers the offline rescue: a link
// already in somebody's inbox must not outlive it.
func TestPostgresRepositoryDeleteByAssistant(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	rescued := seedAssistant(ctx, t, pool, integrationEmail)
	bystander := seedAssistant(ctx, t, pool, "other@email.com")
	now := time.Now().UTC()

	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: rescued,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))
	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationOther,
		AssistantID: bystander,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	revoked, err := repo.DeleteByAssistant(ctx, rescued)
	require.NoError(t, err)
	assert.Equal(t, int64(1), revoked)

	_, err = repo.Get(ctx, integrationDigest)
	assert.ErrorIs(t, err, passwordreset.ErrTokenNotFound)

	surviving, err := repo.Get(ctx, integrationOther)
	require.NoError(t, err)
	assert.Equal(t, integrationOther, surviving.ID)
}

// TestPostgresRepositoryCreateKeepsOneLiveLink pins the unique index: without it
// two concurrent issues for the same account both leave a link behind.
func TestPostgresRepositoryCreateKeepsOneLiveLink(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, integrationEmail)
	now := time.Now().UTC()

	first := passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}
	second := first
	second.ID = integrationOther

	errs := make(chan error, 2)
	go func() { errs <- repo.Create(ctx, first) }()
	go func() { errs <- repo.Create(ctx, second) }()
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	assert.Equal(t, int64(1), countTokens(ctx, t, pool))
}

func TestPostgresRepositoryDeletingAssistantCascades(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	repo := newIntegrationRepository(t, pool)

	assistantID := seedAssistant(ctx, t, pool, integrationEmail)
	now := time.Now().UTC()
	require.NoError(t, repo.Create(ctx, passwordreset.Token{
		ID:          integrationDigest,
		AssistantID: assistantID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(integrationTTL),
	}))

	_, err := pool.Exec(ctx, `DELETE FROM assistant WHERE id = $1`, assistantID)
	require.NoError(t, err)

	assert.Zero(t, countTokens(ctx, t, pool))
}

// TestLinkSurvivesStoreRestart is the reason this package talks to Postgres at
// all: a link mailed before a restart must still be redeemable after it. Store
// holds no state, so a fresh Store over a fresh repository stands in for a new
// process.
func TestLinkSurvivesStoreRestart(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := newIntegrationPool(ctx, t)
	assistantID := seedAssistant(ctx, t, pool, integrationEmail)

	before, err := passwordreset.NewStore(newIntegrationRepository(t, pool), integrationTTL)
	require.NoError(t, err)

	token, err := before.Create(ctx, assistantID)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	var storedID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM public.password_reset_token`).Scan(&storedID))
	assert.NotEqual(t, token, storedID, "the token carried by the link must never be stored verbatim")

	after, err := passwordreset.NewStore(newIntegrationRepository(t, pool), integrationTTL)
	require.NoError(t, err)

	require.NoError(t, after.Verify(ctx, token))

	consumed, err := after.Consume(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, assistantID, consumed)

	err = after.Verify(ctx, token)
	require.Error(t, err)
	assert.ErrorIs(t, err, passwordreset.ErrTokenNotFound)
}
