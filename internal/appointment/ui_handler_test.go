package appointment_test

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/slot"
	"log/slog"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestUIDeps(t *testing.T) (*appointment.Query, *prescription.Query, *professional.Repository, *slot.Query) {
	t.Helper()

	pool := &pgxpool.Pool{}

	query, err := appointment.NewQuery(pool)
	require.NoError(t, err)

	prescriptionQuery, err := prescription.NewQuery(pool)
	require.NoError(t, err)

	profRepo, err := professional.NewRepository(pool)
	require.NoError(t, err)

	slotQuery, err := slot.NewQuery(pool)
	require.NoError(t, err)

	return query, prescriptionQuery, profRepo, slotQuery
}

type newUIHandlerArgs struct {
	logger            *slog.Logger
	service           *mockService
	query             *appointment.Query
	prescriptionQuery *prescription.Query
	profRepo          *professional.Repository
	slotQuery         *slot.Query
}

func TestNewUIHandlerValidation(t *testing.T) {
	t.Parallel()

	query, prescriptionQuery, profRepo, slotQuery := newTestUIDeps(t)
	validArgs := func() newUIHandlerArgs {
		return newUIHandlerArgs{
			logger:            newTestLogger(),
			service:           new(mockService),
			query:             query,
			prescriptionQuery: prescriptionQuery,
			profRepo:          profRepo,
			slotQuery:         slotQuery,
		}
	}

	tests := []struct {
		name     string
		mutate   func(*newUIHandlerArgs)
		expected error
	}{
		{name: "nil logger", mutate: func(a *newUIHandlerArgs) { a.logger = nil }, expected: appointment.ErrNilLogger},
		{name: "nil service", mutate: func(a *newUIHandlerArgs) { a.service = nil }, expected: appointment.ErrNilService},
		{name: "nil query", mutate: func(a *newUIHandlerArgs) { a.query = nil }, expected: appointment.ErrNilQuery},
		{name: "nil prescription query", mutate: func(a *newUIHandlerArgs) { a.prescriptionQuery = nil }, expected: appointment.ErrNilPrescriptionQuery},
		{name: "nil professional repo", mutate: func(a *newUIHandlerArgs) { a.profRepo = nil }, expected: appointment.ErrNilProfessionalRepo},
		{name: "nil slot query", mutate: func(a *newUIHandlerArgs) { a.slotQuery = nil }, expected: appointment.ErrNilSlotQuery},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := validArgs()
			tt.mutate(&args)

			h, err := appointment.NewUIHandler(args.logger, args.service, args.query, args.prescriptionQuery, args.profRepo, args.slotQuery)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestNewUIHandlerSuccess(t *testing.T) {
	t.Parallel()

	query, prescriptionQuery, profRepo, slotQuery := newTestUIDeps(t)

	h, err := appointment.NewUIHandler(newTestLogger(), new(mockService), query, prescriptionQuery, profRepo, slotQuery)

	require.NoError(t, err)
	assert.NotNil(t, h)
}

func TestRegisterUIHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	query, prescriptionQuery, profRepo, slotQuery := newTestUIDeps(t)

	h, err := appointment.NewUIHandler(newTestLogger(), new(mockService), query, prescriptionQuery, profRepo, slotQuery)
	require.NoError(t, err)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterUIHandlers(mux)
	})
}
