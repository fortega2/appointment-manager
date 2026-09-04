//go:build integration

package appointment_test

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/slot"
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	emitTargetSlotDate = "2999-07-01"
	emitOtherSlotDate  = "2999-07-02"
)

func fetchOutboxAggregateIDs(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []uuid.UUID {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT aggregate_id
		FROM public.outbox
		WHERE aggregate_type = $1 AND event_type = $2
		ORDER BY id
	`, string(slot.OutboxAggregate), string(slot.EventCancelled))
	require.NoError(t, err)
	defer rows.Close()

	aggregateIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var aggregateID uuid.UUID
		require.NoError(t, rows.Scan(&aggregateID))
		aggregateIDs = append(aggregateIDs, aggregateID)
	}
	require.NoError(t, rows.Err())

	return aggregateIDs
}

func TestCancelBySlotEmitsOneOutboxEventPerSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)

	firstPatientID := uuid.NewV7()
	secondPatientID := uuid.NewV7()
	insertPatient(ctx, t, pool, firstPatientID)
	insertPatient(ctx, t, pool, secondPatientID)

	slotID := uuid.NewV7()
	insertSlot(ctx, t, pool, slotID, professionalID, emitTargetSlotDate, "09:00:00+00", "09:30:00+00", 10, false)
	insertAppointment(ctx, t, pool, uuid.NewV7(), slotID, firstPatientID, professionalID, assistantID, statusConfirmedValue, nil)
	insertAppointment(ctx, t, pool, uuid.NewV7(), slotID, secondPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.CancelBySlot(ctx, slotID)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	assert.Equal(t, []uuid.UUID{slotID}, fetchOutboxAggregateIDs(ctx, t, pool))

	secondCount, err := repo.CancelBySlot(ctx, slotID)
	require.NoError(t, err)
	require.Zero(t, secondCount)

	assert.Equal(t, []uuid.UUID{slotID}, fetchOutboxAggregateIDs(ctx, t, pool),
		"cancelling nothing must not emit a second event")
}

func TestCancelBySlotEmitsNothingWhenNoAppointmentsCancelled(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.NewV7()
	insertProfessional(ctx, t, pool, professionalID)

	emptySlotID := uuid.NewV7()
	insertSlot(ctx, t, pool, emptySlotID, professionalID, emitTargetSlotDate, "11:00:00+00", "11:30:00+00", 5, false)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.CancelBySlot(ctx, emptySlotID)
	require.NoError(t, err)
	require.Zero(t, count)

	assert.Empty(t, fetchOutboxAggregateIDs(ctx, t, pool))
}

func TestCancelOnBlockedSlotsEmitsOneOutboxEventPerBlockedSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newIntegrationPool(ctx, t)

	professionalID := uuid.NewV7()
	assistantID := uuid.NewV7()
	insertProfessional(ctx, t, pool, professionalID)
	insertAssistant(ctx, t, pool, assistantID)

	firstPatientID := uuid.NewV7()
	secondPatientID := uuid.NewV7()
	openPatientID := uuid.NewV7()
	for _, patientID := range []uuid.UUID{firstPatientID, secondPatientID, openPatientID} {
		insertPatient(ctx, t, pool, patientID)
	}

	firstBlockedSlotID := uuid.NewV7()
	insertSlot(ctx, t, pool, firstBlockedSlotID, professionalID, emitTargetSlotDate, "14:00:00+00", "14:30:00+00", 10, true)
	insertAppointment(ctx, t, pool, uuid.NewV7(), firstBlockedSlotID, firstPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	secondBlockedSlotID := uuid.NewV7()
	insertSlot(ctx, t, pool, secondBlockedSlotID, professionalID, emitOtherSlotDate, "14:00:00+00", "14:30:00+00", 10, true)
	insertAppointment(ctx, t, pool, uuid.NewV7(), secondBlockedSlotID, secondPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	openSlotID := uuid.NewV7()
	insertSlot(ctx, t, pool, openSlotID, professionalID, emitOtherSlotDate, "16:00:00+00", "16:30:00+00", 10, false)
	insertAppointment(ctx, t, pool, uuid.NewV7(), openSlotID, openPatientID, professionalID, assistantID, statusConfirmedValue, nil)

	repo, err := appointment.NewPostgresRepository(pool)
	require.NoError(t, err)

	count, err := repo.CancelOnBlockedSlots(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	assert.ElementsMatch(t,
		[]uuid.UUID{firstBlockedSlotID, secondBlockedSlotID},
		fetchOutboxAggregateIDs(ctx, t, pool),
	)
}
