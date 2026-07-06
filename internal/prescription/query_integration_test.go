//go:build integration

package prescription_test

import (
	"appointment-manager/internal/prescription"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	queryActivePrescriptionSessions    = 8
	queryCancelledPrescriptionSessions = 5
)

func TestQueryListActiveBalancesEligibleAndAvailablePatients(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)
	query, err := prescription.NewQuery(pool)
	require.NoError(t, err)

	withoutPrescription := seedPatientNamed(ctx, t, pool, "Ana", "SinReceta")
	activePatient := seedPatientNamed(ctx, t, pool, "Bruno", "Activo")
	cancelledPatient := seedPatientNamed(ctx, t, pool, "Carla", "Cancelada")

	activeRx, err := prescription.New(activePatient, "prescriptions/bruno.pdf", queryActivePrescriptionSessions)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, activeRx))

	cancelledRx, err := prescription.New(cancelledPatient, "prescriptions/carla.pdf", queryCancelledPrescriptionSessions)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, cancelledRx))
	require.NoError(t, repo.UpdateStatus(ctx, cancelledRx.ID, prescription.StatusCancelled))

	balances, err := query.ListActiveBalances(ctx)
	require.NoError(t, err)
	require.Len(t, balances, 1)
	assert.Equal(t, activePatient.String(), balances[0].PatientID)
	assert.Equal(t, activeRx.ID.String(), balances[0].PrescriptionID)
	assert.Equal(t, queryActivePrescriptionSessions, balances[0].TotalSessions)
	assert.Equal(t, queryActivePrescriptionSessions, balances[0].RemainingSessions)

	eligible, err := query.EligiblePatients(ctx)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	assert.Equal(t, activePatient.String(), eligible[0].ID)

	available, err := query.AvailablePatients(ctx)
	require.NoError(t, err)
	availableIDs := make([]string, len(available))
	for i, o := range available {
		availableIDs[i] = o.ID
	}
	assert.Contains(t, availableIDs, withoutPrescription.String())
	assert.Contains(t, availableIDs, cancelledPatient.String())
	assert.NotContains(t, availableIDs, activePatient.String())
}
