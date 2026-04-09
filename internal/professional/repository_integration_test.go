//go:build integration

package professional_test

import (
	"appointment-manager/internal/professional"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	repositoryFirstName = "Laura"
	repositoryLastName  = "Gomez"
	repositoryPhone     = "1133334444"
)

func TestRepositoryCreatePersistsProfessional(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)

	newRecord, err := professional.NewProfessional(repositoryFirstName, repositoryLastName, repositoryPhone)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.NoError(t, err)

	persisted := fetchProfessionalRecord(ctx, t, pool, newRecord.ID)
	assert.Equal(t, newRecord.ID, persisted.ID)
	assert.Equal(t, repositoryFirstName, persisted.FirstName)
	assert.Equal(t, repositoryLastName, persisted.LastName)
	assert.Equal(t, repositoryPhone, persisted.Phone)
	assert.Equal(t, "kinesiology", persisted.Specialty)
	assert.True(t, persisted.Active)
}

func fetchProfessionalRecord(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID) professional.Professional {
	t.Helper()

	var persisted professional.Professional
	err := pool.QueryRow(ctx, `
		SELECT
			id,
			first_name,
			last_name,
			phone,
			specialty,
			active
		FROM
			professional
		WHERE
			id = $1
	`, id).Scan(
		&persisted.ID,
		&persisted.FirstName,
		&persisted.LastName,
		&persisted.Phone,
		&persisted.Specialty,
		&persisted.Active,
	)
	require.NoError(t, err)

	return persisted
}
