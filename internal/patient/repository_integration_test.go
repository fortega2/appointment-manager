//go:build integration

package patient_test

import (
	"appointment-manager/internal/patient"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	repositoryPatientFirstName       = "Laura"
	repositoryPatientLastName        = "Gomez"
	repositoryPatientPhone           = "1133334444"
	repositoryPatientEmail           = "laura@mail.com"
	repositoryPatientInsurance       = 1
	repositoryPatientInsuranceNumber = "12345678901"
	repositoryPatientNotes           = "dolor lumbar"
	repositoryPatientInvalidIns      = 999
)

func TestRepositoryCreatePersistsPatient(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)

	newRecord, err := patient.NewPatient(
		repositoryPatientFirstName,
		repositoryPatientLastName,
		repositoryPatientPhone,
		repositoryPatientEmail,
		repositoryPatientInsurance,
		repositoryPatientInsuranceNumber,
		stringPtr(repositoryPatientNotes),
	)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.NoError(t, err)

	persisted := fetchPatientRecord(ctx, t, pool, newRecord.ID)
	assert.Equal(t, newRecord.ID, persisted.ID)
	assert.Equal(t, repositoryPatientFirstName, persisted.FirstName)
	assert.Equal(t, repositoryPatientLastName, persisted.LastName)
	assert.Equal(t, repositoryPatientPhone, persisted.Phone)
	assert.Equal(t, repositoryPatientEmail, persisted.Email)
	assert.Equal(t, repositoryPatientInsurance, persisted.HealthInsurance)
	assert.Equal(t, repositoryPatientInsuranceNumber, persisted.InsuranceNumber)
	require.NotNil(t, persisted.ClinicalNotes)
	assert.Equal(t, repositoryPatientNotes, *persisted.ClinicalNotes)
}

func TestRepositoryCreateWithNilClinicalNotes(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)

	newRecord, err := patient.NewPatient(
		repositoryPatientFirstName,
		repositoryPatientLastName,
		repositoryPatientPhone,
		repositoryPatientEmail,
		repositoryPatientInsurance,
		repositoryPatientInsuranceNumber,
		nil,
	)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.NoError(t, err)

	persisted := fetchPatientRecord(ctx, t, pool, newRecord.ID)
	assert.Nil(t, persisted.ClinicalNotes)
}

func TestRepositoryCreateInvalidHealthInsuranceReturnsValidationError(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)

	newRecord, err := patient.NewPatient(
		repositoryPatientFirstName,
		repositoryPatientLastName,
		repositoryPatientPhone,
		repositoryPatientEmail,
		repositoryPatientInvalidIns,
		repositoryPatientInsuranceNumber,
		nil,
	)
	require.NoError(t, err)

	err = repo.Create(ctx, newRecord)
	require.Error(t, err)
	assert.True(t, errors.Is(err, patient.ErrInvalidHealthInsurance))
	assert.Equal(t, int64(0), countPatients(ctx, t, pool))
}

func TestRepositoryCreateReturnsErrorWhenDatabaseUnavailable(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPatientIntegrationPool(ctx, t)
	repo := newPatientIntegrationRepository(t, pool)

	newRecord, err := patient.NewPatient(
		repositoryPatientFirstName,
		repositoryPatientLastName,
		repositoryPatientPhone,
		repositoryPatientEmail,
		repositoryPatientInsurance,
		repositoryPatientInsuranceNumber,
		nil,
	)
	require.NoError(t, err)

	pool.Close()

	err = repo.Create(ctx, newRecord)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create patient")
}

func fetchPatientRecord(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID) patient.Patient {
	t.Helper()

	var persisted patient.Patient
	err := pool.QueryRow(ctx, `
		SELECT
			id,
			first_name,
			last_name,
			phone,
			email,
			health_insurance,
			insurance_number,
			clinical_notes
		FROM
			patient
		WHERE
			id = $1
	`, id).Scan(
		&persisted.ID,
		&persisted.FirstName,
		&persisted.LastName,
		&persisted.Phone,
		&persisted.Email,
		&persisted.HealthInsurance,
		&persisted.InsuranceNumber,
		&persisted.ClinicalNotes,
	)
	require.NoError(t, err)

	return persisted
}

func stringPtr(value string) *string {
	return &value
}
