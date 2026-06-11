package appointment_test

import (
	"appointment-manager/internal/appointment"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	unitAppointmentsPath   = "/api/v1/appointments"
	unitProblemContentType = "application/problem+json"
	unitHeaderContentType  = "Content-Type"
	unitContentTypeJSON    = "application/json"
	boomErrMessage         = "boom"
)

type mockService struct {
	mock.Mock
}

func (m *mockService) List(ctx context.Context, input appointment.ListInput) ([]appointment.Appointment, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]appointment.Appointment), args.Error(1)
}

func (m *mockService) Create(ctx context.Context, input appointment.CreateInput) (uuid.UUID, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return uuid.Nil, args.Error(1)
	}

	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *mockService) Cancel(ctx context.Context, appointmentID uuid.UUID) error {
	args := m.Called(ctx, appointmentID)
	return args.Error(0)
}

func (m *mockService) Attend(ctx context.Context, appointmentID uuid.UUID) error {
	args := m.Called(ctx, appointmentID)
	return args.Error(0)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMuxWithHandler(t *testing.T, service *mockService) *http.ServeMux {
	t.Helper()

	h, err := appointment.NewHandler(newTestLogger(), service)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		logger   *slog.Logger
		service  *mockService
		expected error
	}{
		{name: "nil logger", logger: nil, service: new(mockService), expected: appointment.ErrNilLogger},
		{name: "nil service", logger: newTestLogger(), service: nil, expected: appointment.ErrNilService},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := appointment.NewHandler(tt.logger, tt.service)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestRegisterHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	h, err := appointment.NewHandler(newTestLogger(), new(mockService))
	require.NoError(t, err)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterHandlers(mux)
	})
}

func TestListEndpointInvalidQueryReturnsBadRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		err   error
	}{
		{name: "invalid page", query: "?page=0", err: appointment.ErrInvalidPage},
		{name: "invalid limit", query: "?limit=0", err: appointment.ErrInvalidLimit},
		{name: "invalid status", query: "?status=9", err: appointment.ErrInvalidStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := new(mockService)
			mux := newMuxWithHandler(t, svc)

			svc.On("List", mock.Anything, mock.Anything).Return([]appointment.Appointment(nil), tt.err).Once()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, unitAppointmentsPath+tt.query, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, unitProblemContentType, rec.Header().Get(unitHeaderContentType))
			svc.AssertExpectations(t)
		})
	}
}

func TestListEndpointReturnsInternalServerErrorWhenServiceFails(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	mux := newMuxWithHandler(t, svc)

	svc.On("List", mock.Anything, mock.Anything).Return([]appointment.Appointment(nil), errors.New(boomErrMessage)).Once()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, unitAppointmentsPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, unitProblemContentType, rec.Header().Get(unitHeaderContentType))
	svc.AssertExpectations(t)
}

func TestCreateEndpointValidationAndBusinessErrors(t *testing.T) {
	t.Parallel()

	body := `{"slot_id":"` + uuid.Must(uuid.NewV7()).String() + `","patient_id":"` + uuid.Must(uuid.NewV7()).String() + `","professional_id":"` + uuid.Must(uuid.NewV7()).String() + `","assistant_id":"` + uuid.Must(uuid.NewV7()).String() + `"}`

	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{name: "validation error", err: appointment.ErrInvalidSlotID, expectedStatus: http.StatusUnprocessableEntity},
		{name: "conflict error", err: appointment.ErrSlotBlocked, expectedStatus: http.StatusConflict},
		{name: "invalid reference", err: appointment.ErrInvalidAppointmentReference, expectedStatus: http.StatusUnprocessableEntity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := new(mockService)
			mux := newMuxWithHandler(t, svc)

			svc.On("Create", mock.Anything, mock.Anything).Return(uuid.Nil, tt.err).Once()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, unitAppointmentsPath, bytes.NewBufferString(body))
			req.Header.Set(unitHeaderContentType, unitContentTypeJSON)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			assert.Equal(t, unitProblemContentType, rec.Header().Get(unitHeaderContentType))
			svc.AssertExpectations(t)
		})
	}
}

func TestCancelEndpointValidationAndBusinessErrors(t *testing.T) {
	t.Parallel()

	id := uuid.Must(uuid.NewV7())

	tests := []struct {
		name           string
		path           string
		err            error
		expectedStatus int
		setupMock      bool
	}{
		{name: "invalid id", path: unitAppointmentsPath + "/invalid/cancel", expectedStatus: http.StatusBadRequest, setupMock: false},
		{name: "invalid status transition", path: unitAppointmentsPath + "/" + id.String() + "/cancel", err: appointment.ErrAppointmentCannotCancelWithStatus, expectedStatus: http.StatusConflict, setupMock: true},
		{name: "reference not found", path: unitAppointmentsPath + "/" + id.String() + "/cancel", err: appointment.ErrInvalidAppointmentReference, expectedStatus: http.StatusNotFound, setupMock: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := new(mockService)
			mux := newMuxWithHandler(t, svc)

			if tt.setupMock {
				svc.On("Cancel", mock.Anything, id).Return(tt.err).Once()
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tt.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			assert.Equal(t, unitProblemContentType, rec.Header().Get(unitHeaderContentType))
			svc.AssertExpectations(t)
		})
	}
}

func TestAttendEndpointValidationAndBusinessErrors(t *testing.T) {
	t.Parallel()

	id := uuid.Must(uuid.NewV7())

	tests := []struct {
		name           string
		path           string
		err            error
		expectedStatus int
		setupMock      bool
	}{
		{name: "invalid id", path: unitAppointmentsPath + "/invalid/attend", expectedStatus: http.StatusBadRequest, setupMock: false},
		{name: "outside time window", path: unitAppointmentsPath + "/" + id.String() + "/attend", err: appointment.ErrAppointmentCannotAttendNow, expectedStatus: http.StatusUnprocessableEntity, setupMock: true},
		{name: "invalid status transition", path: unitAppointmentsPath + "/" + id.String() + "/attend", err: appointment.ErrAppointmentCannotAttendWithStatus, expectedStatus: http.StatusConflict, setupMock: true},
		{name: "reference not found", path: unitAppointmentsPath + "/" + id.String() + "/attend", err: appointment.ErrInvalidAppointmentReference, expectedStatus: http.StatusNotFound, setupMock: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := new(mockService)
			mux := newMuxWithHandler(t, svc)

			if tt.setupMock {
				svc.On("Attend", mock.Anything, id).Return(tt.err).Once()
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tt.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			assert.Equal(t, unitProblemContentType, rec.Header().Get(unitHeaderContentType))
			svc.AssertExpectations(t)
		})
	}
}
