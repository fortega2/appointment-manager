//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	pastSlotDate   = "2020-01-01"
	futureSlotDate = "2999-01-01"
)

// ExpireOverdue flips only CONFIRMED appointments whose slot has already ended
// to ABSENT, leaving future and non-CONFIRMED appointments untouched, and is
// idempotent on a second run.
func TestExpireOverdueMarksPastConfirmedAppointmentsAbsent(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.Must(uuid.NewV7())
	assistantID := uuid.Must(uuid.NewV7())
	patientID := uuid.Must(uuid.NewV7())

	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)
	insertPatient(ctx, t, pool, patientID)

	// Past slot, CONFIRMED -> expired to ABSENT.
	pastConfirmedSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, pastConfirmedSlotID, professionalID, pastSlotDate, "09:00:00+00", "09:30:00+00", 1, false)
	pastConfirmedApptID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, pastConfirmedApptID, pastConfirmedSlotID, patientID, professionalID, assistantID, statusConfirmedValue, nil)

	// Past slot, already ATTENDED -> untouched (not CONFIRMED).
	pastAttendedSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, pastAttendedSlotID, professionalID, pastSlotDate, "10:00:00+00", "10:30:00+00", 1, false)
	pastAttendedApptID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, pastAttendedApptID, pastAttendedSlotID, patientID, professionalID, assistantID, statusAttendedValue, nil)

	// Future slot, CONFIRMED -> untouched (slot has not ended).
	futureConfirmedSlotID := uuid.Must(uuid.NewV7())
	insertSlot(ctx, t, pool, futureConfirmedSlotID, professionalID, futureSlotDate, "09:00:00+00", "09:30:00+00", 1, false)
	futureConfirmedApptID := uuid.Must(uuid.NewV7())
	insertAppointment(ctx, t, pool, futureConfirmedApptID, futureConfirmedSlotID, patientID, professionalID, assistantID, statusConfirmedValue, nil)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.ExpireOverdue(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	expiredStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, pastConfirmedApptID)
	assert.Equal(t, statusAbsentValue, expiredStatus)

	attendedStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, pastAttendedApptID)
	assert.Equal(t, statusAttendedValue, attendedStatus)

	futureStatus, _ := fetchAppointmentStatusAndNotes(ctx, t, pool, futureConfirmedApptID)
	assert.Equal(t, statusConfirmedValue, futureStatus)

	// updated_at is stamped on the expired row and left NULL on untouched rows.
	assert.NotNil(t, fetchAppointmentUpdatedAt(ctx, t, pool, pastConfirmedApptID))
	assert.Nil(t, fetchAppointmentUpdatedAt(ctx, t, pool, futureConfirmedApptID))

	// Idempotent: a second sweep finds nothing left to expire.
	secondCount, err := repo.ExpireOverdue(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), secondCount)
}

func fetchAppointmentUpdatedAt(ctx context.Context, t *testing.T, pool *pgxpool.Pool, appointmentID uuid.UUID) *time.Time {
	t.Helper()

	var updatedAt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT
			updated_at
		FROM
			appointment
		WHERE
			id = $1
	`, appointmentID).Scan(&updatedAt)
	require.NoError(t, err)

	return updatedAt
}
