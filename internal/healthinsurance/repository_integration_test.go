//go:build integration

package healthinsurance_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestRepositoryListReturnsHealthInsurances(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newHealthInsuranceIntegrationPool(ctx, t)
	repo := newHealthInsuranceIntegrationRepository(t, pool)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Greater(t, len(list), 0)
}

func TestRepositoryListReturnsErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newHealthInsuranceIntegrationPool(ctx, t)
	repo := newHealthInsuranceIntegrationRepository(t, pool)

	pool.Close()

	list, err := repo.List(ctx)
	require.Error(t, err)
	assert.Nil(t, list)
	assert.Contains(t, err.Error(), "list: query")
}
