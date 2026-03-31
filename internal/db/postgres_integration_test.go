//go:build integration

package db_test

import (
	"appointment-manager/internal/db"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	dbIntegrationImage    = "postgres:18-alpine"
	dbIntegrationName     = "appointment_manager"
	dbIntegrationUser     = "appointment_user"
	dbIntegrationPassword = "appointment_password"
	dbSSLDisableParam     = "sslmode=disable"
	dbAssistantTableName  = "assistant"
)

func TestNewPostgresPoolAppliesMigrations(t *testing.T) {
	ctx := context.Background()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	databaseURL := startPostgresContainer(ctx, t)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var tableName string
	err = pool.QueryRow(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1
	`, dbAssistantTableName).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, dbAssistantTableName, tableName)
}

func startPostgresContainer(ctx context.Context, t *testing.T) string {
	t.Helper()

	container, err := postgres.Run(ctx,
		dbIntegrationImage,
		postgres.WithDatabase(dbIntegrationName),
		postgres.WithUsername(dbIntegrationUser),
		postgres.WithPassword(dbIntegrationPassword),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, dbSSLDisableParam)
	require.NoError(t, err)

	return databaseURL
}
