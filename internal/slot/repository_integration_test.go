//go:build integration

package slot_test

import (
	"appointment-manager/internal/slot"
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	repositoryProfessionalMax = int16(5)
	repositoryStartTime       = "2026-05-25T10:00:00Z"
	repositoryEndTime         = "2026-05-25T11:00:00Z"
)

func TestRepositoryCreatePersistsSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	repo := newSlotIntegrationRepository(t, pool)

	professionalID := uuid.NewV7()
	insertProfessionalForSlot(ctx, t, pool, professionalID)

	startTime := mustParseTime(repositoryStartTime)
	endTime := mustParseTime(repositoryEndTime)

	newRecord, err := slot.NewSlot(professionalID, integrationDate, startTime, endTime, repositoryProfessionalMax)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.NoError(t, err)

	persisted := fetchSlotRecord(ctx, t, pool, newRecord.ID)
	assert.Equal(t, newRecord.ID, persisted.ID)
	assert.Equal(t, professionalID, persisted.ProfessionalID)
	assert.True(t, integrationDate.Equal(persisted.Date), "date mismatch")
	assert.True(t, startTime.Equal(persisted.StartTime), "start_time mismatch")
	assert.True(t, endTime.Equal(persisted.EndTime), "end_time mismatch")
	assert.Equal(t, repositoryProfessionalMax, persisted.MaxCapacity)
	assert.False(t, persisted.Blocked)
}

func TestRepositoryCreateReturnsErrorWhenProfessionalNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	repo := newSlotIntegrationRepository(t, pool)

	startTime := mustParseTime(repositoryStartTime)
	endTime := mustParseTime(repositoryEndTime)

	newRecord, err := slot.NewSlot(uuid.NewV7(), integrationDate, startTime, endTime, repositoryProfessionalMax)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.Error(t, err)
	assert.ErrorIs(t, err, slot.ErrInvalidProfessionalID)
}

func TestRepositoryCancelBlocksSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	repo := newSlotIntegrationRepository(t, pool)

	professionalID := uuid.NewV7()
	insertProfessionalForSlot(ctx, t, pool, professionalID)

	startTime := mustParseTime(repositoryStartTime)
	endTime := mustParseTime(repositoryEndTime)

	newRecord, err := slot.NewSlot(professionalID, integrationDate, startTime, endTime, repositoryProfessionalMax)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, newRecord))

	err = repo.Cancel(ctx, newRecord.ID)
	require.NoError(t, err)

	persisted := fetchSlotRecord(ctx, t, pool, newRecord.ID)
	assert.True(t, persisted.Blocked)
}

func TestRepositoryCancelReturnsErrorWhenAlreadyCancelled(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	repo := newSlotIntegrationRepository(t, pool)

	professionalID := uuid.NewV7()
	insertProfessionalForSlot(ctx, t, pool, professionalID)

	startTime := mustParseTime(repositoryStartTime)
	endTime := mustParseTime(repositoryEndTime)

	newRecord, err := slot.NewSlot(professionalID, integrationDate, startTime, endTime, repositoryProfessionalMax)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, newRecord))
	require.NoError(t, repo.Cancel(ctx, newRecord.ID))

	err = repo.Cancel(ctx, newRecord.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, slot.ErrSlotAlreadyCancelled)
}

func TestRepositoryCancelReturnsErrorWhenNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	repo := newSlotIntegrationRepository(t, pool)

	err := repo.Cancel(ctx, uuid.NewV7())
	require.Error(t, err)
	assert.ErrorIs(t, err, slot.ErrSlotNotFound)
}

func TestRepositoryCreateReturnsErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	repo := newSlotIntegrationRepository(t, pool)

	professionalID := uuid.NewV7()
	insertProfessionalForSlot(ctx, t, pool, professionalID)

	startTime := mustParseTime(repositoryStartTime)
	endTime := mustParseTime(repositoryEndTime)

	newRecord, err := slot.NewSlot(professionalID, integrationDate, startTime, endTime, repositoryProfessionalMax)
	require.NoError(t, err)

	pool.Close()

	err = repo.Create(ctx, newRecord)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create slot:")
}
