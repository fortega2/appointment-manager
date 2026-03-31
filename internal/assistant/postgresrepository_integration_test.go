//go:build integration

package assistant_test

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/db"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	repoIntegrationImage    = "postgres:18-alpine"
	repoIntegrationName     = "appointment_manager"
	repoIntegrationUser     = "appointment_user"
	repoIntegrationPassword = "appointment_password"
	repoSSLDisableParam     = "sslmode=disable"
	repoNamesFmt            = "Assistant-%d"
	repoLastNames           = "Test"
	repoEmailFmt            = "assistant-%d@email.com"
	repoPasswordHashFmt     = "hash-%d"
	repoMissingIDLiteral    = "11111111-1111-1111-1111-111111111111"
)

func TestPostgresRepositoryCreateListGet(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	repo := newIntegrationRepository(t, ctx)

	createdIDs := make([]uuid.UUID, 0, 2)
	for i := range 2 {
		newID := uuid.New()
		createdID, err := repo.Create(ctx, assistant.Assistant{
			ID:           newID,
			FirstName:    fmt.Sprintf(repoNamesFmt, i),
			LastName:     repoLastNames,
			Email:        fmt.Sprintf(repoEmailFmt, i),
			PasswordHash: fmt.Sprintf(repoPasswordHashFmt, i),
		})
		require.NoError(t, err)
		assert.Equal(t, newID, createdID)
		createdIDs = append(createdIDs, createdID)
	}

	assistants, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, assistants, len(createdIDs))

	for i, id := range createdIDs {
		record, getErr := repo.Get(ctx, id)
		require.NoError(t, getErr)
		require.NotNil(t, record)
		assert.Equal(t, id, record.ID)
		assert.Equal(t, fmt.Sprintf(repoNamesFmt, i), record.FirstName)
		assert.Equal(t, repoLastNames, record.LastName)
		assert.Equal(t, fmt.Sprintf(repoEmailFmt, i), record.Email)
		assert.Equal(t, fmt.Sprintf(repoPasswordHashFmt, i), record.PasswordHash)
	}
}

func TestPostgresRepositoryGetNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	repo := newIntegrationRepository(t, ctx)

	missingID := uuid.MustParse(repoMissingIDLiteral)
	record, err := repo.Get(ctx, missingID)

	require.Error(t, err)
	assert.Nil(t, record)
	assert.True(t, errors.Is(err, assistant.ErrAssistantNotFound))
}

func newIntegrationRepository(t *testing.T, ctx context.Context) *assistant.PostgresRepository {
	t.Helper()

	databaseURL := startAssistantPostgresContainer(t, ctx)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo, err := assistant.NewPostgresRepository(pool)
	require.NoError(t, err)

	return repo
}

func startAssistantPostgresContainer(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := postgres.Run(ctx,
		repoIntegrationImage,
		postgres.WithDatabase(repoIntegrationName),
		postgres.WithUsername(repoIntegrationUser),
		postgres.WithPassword(repoIntegrationPassword),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, repoSSLDisableParam)
	require.NoError(t, err)

	return databaseURL
}
