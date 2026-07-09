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

func TestRepositoryListReturnsOnlyActiveProfessionals(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)

	activeID := uuid.Must(uuid.NewV7())
	inactiveID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, activeID, "Laura", "Gomez", "1111111111", true)
	insertProfessional(ctx, t, pool, inactiveID, "Ana", "Perez", "2222222222", false)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	assert.Equal(t, activeID, list[0].ID)
	assert.Equal(t, "Laura", list[0].FirstName)
	assert.Equal(t, "Gomez", list[0].LastName)
	assert.Equal(t, "1111111111", list[0].Phone)
	assert.Equal(t, "Kinesiology", list[0].Specialty)
	assert.True(t, list[0].Active)
}

func TestRepositoryListReturnsEmptySliceWhenNoActiveProfessionals(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)

	insertProfessional(ctx, t, pool, uuid.Must(uuid.NewV7()), "Laura", "Gomez", "1111111111", false)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRepositoryListReturnsErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newProfessionalIntegrationPool(ctx, t)
	repo := newProfessionalIntegrationRepository(t, pool)

	pool.Close()

	list, err := repo.List(ctx)
	require.Error(t, err)
	assert.Nil(t, list)
	assert.Contains(t, err.Error(), "query professionals")
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

func insertProfessional(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	id uuid.UUID,
	firstName string,
	lastName string,
	phone string,
	active bool,
) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO professional (id, first_name, last_name, phone, specialty, active)
		VALUES ($1, $2, $3, $4, 'kinesiology', $5)
	`, id, firstName, lastName, phone, active)
	require.NoError(t, err)
}
