package assistant_test

import (
	"appointment-manager/internal/assistant"
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
	handlerAssistantsPath           = "/api/v1/assistants"
	handlerAssistantNames           = "Jane"
	handlerAssistantLastNames       = "Doe"
	handlerAssistantEmail           = "jane.doe@email.com"
	handlerAssistantFixedID         = "a6f3d3f9-6dd2-4848-8af9-98fe0c060816"
	handlerInvalidAssistantID       = "not-an-id"
	handlerAssistantPlainPassword   = "123456"
	handlerBoomErrMsg               = "boom"
	handlerCreateAssistantBody      = `{"first_name":"Jane","last_name":"Doe","email":"jane.doe@email.com","password":"123456"}`
	handlerCreateAssistantBadBody   = `{"first_name":"","last_name":"Doe","email":"jane.doe@email.com","password":"123456"}`
	handlerCaseServiceError         = "service error"
	handlerCaseSuccess              = "success"
	handlerWrappedErrMsg            = "wrapped"
	handlerContentType              = "Content-Type"
	handlerApplicationJSON          = "application/json"
	handlerLocationHeader           = "Location"
	handlerCaseInvalidID            = "invalid id"
	handlerCaseNotFound             = "not found"
	handlerCaseValidationError      = "validation error"
	handlerCaseEmptyHashError       = "empty hash error"
	handlerCaseInvalidJSON          = "invalid json"
	handlerCaseNilLogger            = "nil logger"
	handlerCaseNilService           = "nil service"
	handlerCaseRegisterDoesNotPanic = "register does not panic"
)

type mockService struct {
	mock.Mock
}

func (m *mockService) List(ctx context.Context) ([]assistant.Assistant, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]assistant.Assistant), args.Error(1)
}

func (m *mockService) Get(ctx context.Context, id uuid.UUID) (*assistant.Assistant, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*assistant.Assistant), args.Error(1)
}

func (m *mockService) Create(ctx context.Context, input assistant.CreateInput) (uuid.UUID, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return uuid.Nil, args.Error(1)
	}

	return args.Get(0).(uuid.UUID), args.Error(1)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMuxWithHandler(t *testing.T, service *mockService) *http.ServeMux {
	t.Helper()

	h, err := assistant.NewHandler(newTestLogger(), service)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	logger := newTestLogger()

	tests := []struct {
		name     string
		logger   *slog.Logger
		service  *mockService
		expected error
	}{
		{name: handlerCaseNilLogger, logger: nil, service: svc, expected: assistant.ErrNilLogger},
		{name: handlerCaseNilService, logger: logger, service: nil, expected: assistant.ErrNilService},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := assistant.NewHandler(tt.logger, tt.service)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestRegisterHandlers(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseRegisterDoesNotPanic, func(t *testing.T) {
		t.Parallel()

		h, err := assistant.NewHandler(newTestLogger(), new(mockService))
		require.NoError(t, err)

		mux := http.NewServeMux()
		assert.NotPanics(t, func() {
			h.RegisterHandlers(mux)
		})
	})
}

func TestListEndpoint(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseServiceError, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		mux := newMuxWithHandler(t, svc)

		svc.On("List", mock.Anything).Return([]assistant.Assistant(nil), errors.New(handlerBoomErrMsg)).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseSuccess, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		mux := newMuxWithHandler(t, svc)

		recordID := uuid.MustParse(handlerAssistantFixedID)
		expected := []assistant.Assistant{{
			ID:           recordID,
			FirstName:    handlerAssistantNames,
			LastName:     handlerAssistantLastNames,
			Email:        handlerAssistantEmail,
			PasswordHash: "hidden",
		}}

		svc.On("List", mock.Anything).Return(expected, nil).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, handlerApplicationJSON, rec.Header().Get(handlerContentType))
		assert.Contains(t, rec.Body.String(), handlerAssistantEmail)
		assert.NotContains(t, rec.Body.String(), "hidden")
		svc.AssertExpectations(t)
	})
}

func TestGetEndpoint(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseInvalidID, func(t *testing.T) {
		t.Parallel()

		mux := newMuxWithHandler(t, new(mockService))

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+handlerInvalidAssistantID, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run(handlerCaseNotFound, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		mux := newMuxWithHandler(t, svc)

		missingID := uuid.MustParse(handlerAssistantFixedID)
		svc.On("Get", mock.Anything, missingID).Return((*assistant.Assistant)(nil), errors.Join(assistant.ErrAssistantNotFound, errors.New(handlerWrappedErrMsg))).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+missingID.String(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseServiceError, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		mux := newMuxWithHandler(t, svc)

		recordID := uuid.MustParse(handlerAssistantFixedID)
		svc.On("Get", mock.Anything, recordID).Return((*assistant.Assistant)(nil), errors.New(handlerBoomErrMsg)).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+recordID.String(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseSuccess, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		mux := newMuxWithHandler(t, svc)

		record := &assistant.Assistant{
			ID:           uuid.MustParse(handlerAssistantFixedID),
			FirstName:    handlerAssistantNames,
			LastName:     handlerAssistantLastNames,
			Email:        handlerAssistantEmail,
			PasswordHash: "hidden",
		}

		svc.On("Get", mock.Anything, record.ID).Return(record, nil).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+record.ID.String(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, handlerApplicationJSON, rec.Header().Get(handlerContentType))
		assert.Contains(t, rec.Body.String(), handlerAssistantEmail)
		assert.NotContains(t, rec.Body.String(), "hidden")
		svc.AssertExpectations(t)
	})
}

func TestCreateEndpoint(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseInvalidJSON, func(t *testing.T) {
		t.Parallel()

		mux := newMuxWithHandler(t, new(mockService))

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString("{"))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run(handlerCaseValidationError, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		svc.On("Create", mock.Anything, assistant.CreateInput{
			FirstName: handlerAssistantNames,
			LastName:  handlerAssistantLastNames,
			Email:     handlerAssistantEmail,
			Password:  handlerAssistantPlainPassword,
		}).Return(uuid.Nil, assistant.ErrFirstNameRequired).Once()

		mux := newMuxWithHandler(t, svc)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseServiceError, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		svc.On("Create", mock.Anything, assistant.CreateInput{
			FirstName: handlerAssistantNames,
			LastName:  handlerAssistantLastNames,
			Email:     handlerAssistantEmail,
			Password:  handlerAssistantPlainPassword,
		}).Return(uuid.Nil, errors.New(handlerBoomErrMsg)).Once()

		mux := newMuxWithHandler(t, svc)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseEmptyHashError, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		svc.On("Create", mock.Anything, assistant.CreateInput{
			FirstName: handlerAssistantNames,
			LastName:  handlerAssistantLastNames,
			Email:     handlerAssistantEmail,
			Password:  handlerAssistantPlainPassword,
		}).Return(uuid.Nil, assistant.ErrEmptyPasswordHash).Once()

		mux := newMuxWithHandler(t, svc)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseSuccess, func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		createdID := uuid.MustParse(handlerAssistantFixedID)
		svc.On("Create", mock.Anything, assistant.CreateInput{
			FirstName: handlerAssistantNames,
			LastName:  handlerAssistantLastNames,
			Email:     handlerAssistantEmail,
			Password:  handlerAssistantPlainPassword,
		}).Return(createdID, nil).Once()

		mux := newMuxWithHandler(t, svc)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, handlerApplicationJSON, rec.Header().Get(handlerContentType))
		assert.Equal(t, handlerAssistantsPath+"/"+createdID.String(), rec.Header().Get(handlerLocationHeader))
		svc.AssertExpectations(t)
	})

	t.Run(handlerCaseValidationError+" missing names", func(t *testing.T) {
		t.Parallel()

		svc := new(mockService)
		svc.On("Create", mock.Anything, assistant.CreateInput{
			FirstName: "",
			LastName:  handlerAssistantLastNames,
			Email:     handlerAssistantEmail,
			Password:  handlerAssistantPlainPassword,
		}).Return(uuid.Nil, assistant.ErrFirstNameRequired).Once()

		mux := newMuxWithHandler(t, svc)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBadBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		svc.AssertExpectations(t)
	})
}
