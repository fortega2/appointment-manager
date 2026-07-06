package prescription

import (
	"context"
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockService struct {
	mock.Mock
}

func (m *mockService) Create(ctx context.Context, patientID uuid.UUID, totalSessions int, file multipart.File, header *multipart.FileHeader) (*Prescription, error) {
	args := m.Called(ctx, patientID, totalSessions, file, header)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*Prescription), args.Error(1)
}

func (m *mockService) Cancel(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockService) PresignedGetURL(ctx context.Context, id uuid.UUID, expiry time.Duration) (string, error) {
	args := m.Called(ctx, id, expiry)
	return args.String(0), args.Error(1)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewUIHandlerValidation(t *testing.T) {
	t.Parallel()

	query := &Query{}

	tests := []struct {
		name     string
		logger   *slog.Logger
		service  *mockService
		query    *Query
		expected error
	}{
		{name: "nil logger", logger: nil, service: new(mockService), query: query, expected: ErrNilLogger},
		{name: "nil service", logger: newTestLogger(), service: nil, query: query, expected: ErrNilService},
		{name: "nil query", logger: newTestLogger(), service: new(mockService), query: nil, expected: ErrNilQuery},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var svc service
			if tt.service != nil {
				svc = tt.service
			}

			h, err := NewUIHandler(tt.logger, svc, tt.query)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestFileRedirectUIHandlerNotFound(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	prescriptionID := uuid.Must(uuid.NewV7())
	svc.On("PresignedGetURL", mock.Anything, prescriptionID, presignExpiry).Return("", ErrPrescriptionNotFound)

	h, err := NewUIHandler(newTestLogger(), svc, &Query{})
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterUIHandlers(mux)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/prescriptions/"+prescriptionID.String()+"/file", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestResolveCreateProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{name: "nil patient id", err: ErrNilPatientID, expectedStatus: http.StatusBadRequest},
		{name: "empty file path", err: ErrEmptyFilePath, expectedStatus: http.StatusBadRequest},
		{name: "invalid total sessions", err: ErrInvalidTotalSessions, expectedStatus: http.StatusBadRequest},
		{name: "unsupported file type", err: ErrUnsupportedFileType, expectedStatus: http.StatusUnprocessableEntity},
		{name: "invalid patient", err: ErrInvalidPatient, expectedStatus: http.StatusUnprocessableEntity},
		{name: "active prescription exists", err: ErrActivePrescriptionExists, expectedStatus: http.StatusConflict},
		{name: "unknown error", err: errors.New("boom"), expectedStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, msg := resolveCreateProblem(tt.err)

			assert.Equal(t, tt.expectedStatus, status)
			assert.NotEmpty(t, msg)
		})
	}
}

func TestResolveCancelProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{name: "not found", err: ErrPrescriptionNotFound, expectedStatus: http.StatusNotFound},
		{name: "unknown error", err: errors.New("boom"), expectedStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, msg := resolveCancelProblem(tt.err)

			assert.Equal(t, tt.expectedStatus, status)
			assert.NotEmpty(t, msg)
		})
	}
}
