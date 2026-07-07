//go:build integration

package prescription_test

import (
	"appointment-manager/internal/prescription"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	repositoryPrescriptionFilePath = "prescriptions/laura.pdf"
	repositoryPrescriptionSessions = 10
)

func TestRepositoryCreatePersistsPrescription(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)
	patientID := seedPatient(ctx, t, pool)

	newRecord, err := prescription.New(patientID, repositoryPrescriptionFilePath, repositoryPrescriptionSessions)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.NoError(t, err)

	persisted := fetchPrescriptionRecord(ctx, t, pool, newRecord.ID)
	assert.Equal(t, newRecord.ID, persisted.ID)
	assert.Equal(t, patientID, persisted.PatientID)
	assert.Equal(t, repositoryPrescriptionFilePath, persisted.FilePath)
	assert.Equal(t, repositoryPrescriptionSessions, persisted.TotalSessions)
	assert.Equal(t, prescription.StatusActive, persisted.Status)
	assert.Equal(t, int64(1), countPrescriptions(ctx, t, pool))
}

func TestRepositoryCreateSecondActiveReturnsError(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)
	patientID := seedPatient(ctx, t, pool)

	first, err := prescription.New(patientID, repositoryPrescriptionFilePath, repositoryPrescriptionSessions)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, first))

	second, err := prescription.New(patientID, "prescriptions/laura-2.pdf", 5)
	require.NoError(t, err)

	err = repo.Create(ctx, second)
	require.Error(t, err)
	assert.ErrorIs(t, err, prescription.ErrActivePrescriptionExists)
	assert.Equal(t, int64(1), countPrescriptions(ctx, t, pool))
}

func TestRepositoryCreateInvalidPatientReturnsError(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)

	// A random patient ID that was never seeded violates the foreign key.
	newRecord, err := prescription.New(uuid.Must(uuid.NewV7()), repositoryPrescriptionFilePath, repositoryPrescriptionSessions)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.Error(t, err)
	assert.ErrorIs(t, err, prescription.ErrInvalidPatient)
	assert.Equal(t, int64(0), countPrescriptions(ctx, t, pool))
}

func TestRepositoryGetByIDNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)

	found, err := repo.GetByID(ctx, uuid.Must(uuid.NewV7()))
	require.Error(t, err)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, prescription.ErrPrescriptionNotFound)
}

func TestRepositoryUpdateStatusCancelAllowsNewActive(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)
	patientID := seedPatient(ctx, t, pool)

	first, err := prescription.New(patientID, repositoryPrescriptionFilePath, repositoryPrescriptionSessions)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, first))

	// Cancelling the active prescription must free the partial unique index
	// so the patient can be assigned a new active prescription.
	require.NoError(t, repo.UpdateStatus(ctx, first.ID, prescription.StatusCancelled))

	second, err := prescription.New(patientID, "prescriptions/laura-2.pdf", 5)
	require.NoError(t, err)

	err = repo.Create(ctx, second)
	require.NoError(t, err)

	active, err := repo.GetByID(ctx, second.ID)
	require.NoError(t, err)
	assert.Equal(t, prescription.StatusActive, active.Status)
	assert.Equal(t, int64(2), countPrescriptions(ctx, t, pool))
}

func TestRepositoryUpdateStatusNotFound(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)

	err := repo.UpdateStatus(ctx, uuid.Must(uuid.NewV7()), prescription.StatusCancelled)
	require.Error(t, err)
	assert.ErrorIs(t, err, prescription.ErrPrescriptionNotFound)
}

func fetchPrescriptionRecord(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID) prescription.Prescription {
	t.Helper()

	var persisted prescription.Prescription
	err := pool.QueryRow(ctx, `
		SELECT
			id,
			patient_id,
			file_path,
			total_sessions,
			status
		FROM
			prescription
		WHERE
			id = $1
	`, id).Scan(
		&persisted.ID,
		&persisted.PatientID,
		&persisted.FilePath,
		&persisted.TotalSessions,
		&persisted.Status,
	)
	require.NoError(t, err)

	return persisted
}
