package slot_test

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/slot"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	invalidSlotIDURL   = "/slots/not-a-uuid"
	invalidSlotIDMsgES = "El identificador del horario no es válido"
	invalidSlotIDMsgEN = "Invalid slot ID"
)

func newHandlerLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func noopCancelAppointments(_ context.Context, _ uuid.UUID) error { return nil }

func noopSendNotification(_ context.Context, _ uuid.UUID) {}

// recordedCalls captures what the two injected collaborators were asked to do,
// so tests can assert the handler's orchestration rather than reaching into the
// appointment or notification packages. Shared with the integration tests.
type recordedCalls struct {
	cancelErr        error
	cancelledSlotIDs []uuid.UUID
	notifiedSlotIDs  []uuid.UUID
}

func (c *recordedCalls) cancelAppointments(_ context.Context, slotID uuid.UUID) error {
	c.cancelledSlotIDs = append(c.cancelledSlotIDs, slotID)

	return c.cancelErr
}

func (c *recordedCalls) sendNotification(_ context.Context, slotID uuid.UUID) {
	c.notifiedSlotIDs = append(c.notifiedSlotIDs, slotID)
}

type newHandlerArgs struct {
	logger             *slog.Logger
	repo               *slot.Repository
	query              *slot.Query
	pRepo              *professional.Repository
	cancelAppointments func(context.Context, uuid.UUID) error
	sendNotification   func(context.Context, uuid.UUID)
}

func validHandlerArgs() newHandlerArgs {
	return newHandlerArgs{
		logger:             newHandlerLogger(),
		repo:               &slot.Repository{},
		query:              &slot.Query{},
		pRepo:              &professional.Repository{},
		cancelAppointments: noopCancelAppointments,
		sendNotification:   noopSendNotification,
	}
}

// The handler refuses to start unless every collaborator is supplied. Both
// cancelAppointments and sendNotification are invoked unconditionally on the
// cancel path, so a nil one would panic at request time rather than at boot.
func TestNewHandlerValidatesDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate   func(*newHandlerArgs)
		expected error
		name     string
	}{
		{name: "nil logger", mutate: func(a *newHandlerArgs) { a.logger = nil }, expected: slot.ErrNilLogger},
		{name: "nil repository", mutate: func(a *newHandlerArgs) { a.repo = nil }, expected: slot.ErrNilRepository},
		{name: "nil query", mutate: func(a *newHandlerArgs) { a.query = nil }, expected: slot.ErrNilQuery},
		{name: "nil professional repository", mutate: func(a *newHandlerArgs) { a.pRepo = nil }, expected: slot.ErrNilProfessionalRepo},
		{name: "nil cancel appointments", mutate: func(a *newHandlerArgs) { a.cancelAppointments = nil }, expected: slot.ErrNilCancelAppointments},
		{name: "nil send notification", mutate: func(a *newHandlerArgs) { a.sendNotification = nil }, expected: slot.ErrNilSendNotification},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := validHandlerArgs()
			tt.mutate(&args)

			h, err := slot.NewHandler(args.logger, args.repo, args.query, args.pRepo, args.cancelAppointments, args.sendNotification)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestNewHandlerSucceedsWithEveryDependency(t *testing.T) {
	t.Parallel()

	args := validHandlerArgs()

	h, err := slot.NewHandler(args.logger, args.repo, args.query, args.pRepo, args.cancelAppointments, args.sendNotification)

	require.NoError(t, err)
	require.NotNil(t, h)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterUIHandlers(mux)
	})
}

// A malformed slot id is rejected before any collaborator runs, so neither the
// appointments nor the notification side effect is triggered. The snackbar copy
// is asserted per locale, since it now comes from the catalog.
func TestCancelUIHandlerRejectsInvalidSlotID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		locale i18n.Locale
		want   string
	}{
		{"spanish", i18n.LocaleES, invalidSlotIDMsgES},
		{"english", i18n.LocaleEN, invalidSlotIDMsgEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := &recordedCalls{}

			args := validHandlerArgs()
			h, err := slot.NewHandler(args.logger, args.repo, args.query, args.pRepo, calls.cancelAppointments, calls.sendNotification)
			require.NoError(t, err)

			mux := http.NewServeMux()
			h.RegisterUIHandlers(mux)

			ctx := i18n.WithLocale(t.Context(), tt.locale)
			req := httptest.NewRequestWithContext(ctx, http.MethodDelete, invalidSlotIDURL, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
			assert.Empty(t, calls.cancelledSlotIDs, "appointments must not be cancelled for an unparseable slot id")
			assert.Empty(t, calls.notifiedSlotIDs, "no notification must be sent for an unparseable slot id")
		})
	}
}
