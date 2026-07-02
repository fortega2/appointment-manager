//go:build integration

package prescription_test

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/prescription"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	prescriptionIntegrationImage    = "postgres:18.3-alpine3.23"
	prescriptionIntegrationDBName   = "appointment_manager"
	prescriptionIntegrationDBUser   = "appointment_user"
	prescriptionIntegrationDBPass   = "appointment_password"
	prescriptionIntegrationSSLParam = "sslmode=disable"

	seedPatientHealthInsurance = 1
	seedPatientInsuranceNumber = "12345678901"
)

func newPrescriptionIntegrationPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := postgres.Run(ctx,
		prescriptionIntegrationImage,
		postgres.WithDatabase(prescriptionIntegrationDBName),
		postgres.WithUsername(prescriptionIntegrationDBUser),
		postgres.WithPassword(prescriptionIntegrationDBPass),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	databaseURL, err := container.ConnectionString(ctx, prescriptionIntegrationSSLParam)
	require.NoError(t, err)

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func newPrescriptionIntegrationRepository(t *testing.T, pool *pgxpool.Pool) *prescription.Repository {
	t.Helper()

	repo, err := prescription.NewRepository(pool)
	require.NoError(t, err)

	return repo
}

// seedPatient inserts a minimal valid patient row and returns its ID so
// prescriptions (which reference patient via a foreign key) can be created.
func seedPatient(ctx context.Context, t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(ctx, `
		INSERT INTO patient (
			id,
			first_name,
			last_name,
			phone,
			email,
			health_insurance,
			insurance_number
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		id,
		"Laura",
		"Gomez",
		"1133334444",
		"laura@mail.com",
		seedPatientHealthInsurance,
		seedPatientInsuranceNumber,
	)
	require.NoError(t, err)

	return id
}

func countPrescriptions(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var total int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM prescription`).Scan(&total)
	require.NoError(t, err)

	return total
}
