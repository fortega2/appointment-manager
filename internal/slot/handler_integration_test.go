//go:build integration

package slot_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"appointment-manager/internal/i18n"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func doCancel(ctx context.Context, t *testing.T, mux *http.ServeMux, slotID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	// The locale middleware is not in this mux, so the language is pinned here:
	// the assertions below are on English copy.
	ctx = i18n.WithLocale(ctx, i18n.LocaleEN)
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodDelete, integrationSlotsPath+slotID.String(), nil))

	return rec
}

// The happy path blocks the slot, cancels its appointments and announces the
// cancellation, all for the same slot id.
func TestCancelUIHandlerCancelsSlotAndItsAppointments(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	slotID := seedCancellableSlot(ctx, t, pool)
	calls := &recordedCalls{}

	rec := doCancel(ctx, t, newSlotIntegrationMux(t, pool, calls), slotID)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Slot cancelled successfully")

	assert.True(t, fetchSlotRecord(ctx, t, pool, slotID).Blocked, "slot must be blocked")
	assert.Equal(t, []uuid.UUID{slotID}, calls.cancelledSlotIDs)
	assert.Equal(t, []uuid.UUID{slotID}, calls.notifiedSlotIDs)
}

// A failed appointment cascade is reported as an error and, crucially, sends no
// notification: the message announces cancelled appointments, so firing it here
// would tell patients their appointment is off while it is still confirmed.
func TestCancelUIHandlerDoesNotNotifyWhenCascadeFails(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	slotID := seedCancellableSlot(ctx, t, pool)
	calls := &recordedCalls{cancelErr: errors.New("appointments unavailable")}

	rec := doCancel(ctx, t, newSlotIntegrationMux(t, pool, calls), slotID)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "Failed to cancel associated appointments")

	assert.True(t, fetchSlotRecord(ctx, t, pool, slotID).Blocked, "slot must stay blocked")
	assert.Equal(t, []uuid.UUID{slotID}, calls.cancelledSlotIDs)
	assert.Empty(t, calls.notifiedSlotIDs, "patients must not be told before their appointments are cancelled")
}

// Cancelling an already-cancelled slot conflicts, and must not cascade: no
// appointments are touched and nobody is notified a second time.
func TestCancelUIHandlerRejectsAlreadyCancelledSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	slotID := seedCancellableSlot(ctx, t, pool)
	calls := &recordedCalls{}
	mux := newSlotIntegrationMux(t, pool, calls)

	require.Equal(t, http.StatusOK, doCancel(ctx, t, mux, slotID).Code)

	rec := doCancel(ctx, t, mux, slotID)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already been cancelled")
	assert.Len(t, calls.cancelledSlotIDs, 1, "a conflicting cancel must not re-cancel appointments")
	assert.Len(t, calls.notifiedSlotIDs, 1, "a conflicting cancel must not re-notify")
}

// A slot that does not exist is a 404 and has no side effects at all.
func TestCancelUIHandlerReturnsNotFoundForUnknownSlot(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newSlotIntegrationPool(ctx, t)
	calls := &recordedCalls{}

	rec := doCancel(ctx, t, newSlotIntegrationMux(t, pool, calls), uuid.Must(uuid.NewV7()))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, calls.cancelledSlotIDs)
	assert.Empty(t, calls.notifiedSlotIDs)
}
