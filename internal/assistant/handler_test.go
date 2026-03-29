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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	handlerAssistantsPath           = "/api/v1/assistants"
	handlerAssistantNames           = "Jane"
	handlerAssistantLastNames       = "Doe"
	handlerAssistantEmail           = "jane.doe@email.com"
	handlerAssistantHash            = "hash"
	handlerAssistantHashedPassword  = "hashed"
	handlerAssistantHiddenPassword  = "hidden"
	handlerAssistantPlainPassword   = "123456"
	handlerAssistantFixedID         = "a6f3d3f9-6dd2-4848-8af9-98fe0c060816"
	handlerInvalidAssistantID       = "not-an-id"
	handlerBoomErrMsg               = "boom"
	handlerCreateAssistantBody      = `{"names":"Jane","last_names":"Doe","email":"jane.doe@email.com","password":"123456"}`
	handlerCreateAssistantBadBody   = `{"names":"","last_names":"Doe","email":"jane.doe@email.com","password":"123456"}`
	handlerCaseRepositoryError      = "repository error"
	handlerCaseSuccess              = "success"
	handlerPathIDParam              = "id"
	handlerWrappedErrMsg            = "wrapped"
	handlerContentType              = "Content-Type"
	handlerApplicationJSON          = "application/json"
	handlerLocationHeader           = "Location"
	handlerCaseInvalidID            = "invalid id"
	handlerCaseNotFound             = "not found"
	handlerCaseHashingError         = "hashing error"
	handlerCaseValidationError      = "validation error"
	handlerCaseInvalidJSON          = "invalid json"
	handlerCaseNilLogger            = "nil logger"
	handlerCaseNilRepository        = "nil repository"
	handlerCaseNilHasher            = "nil hasher"
	handlerCaseRegisterDoesNotPanic = "register does not panic"
	handlerGosecIgnoreReason        = "G101: false positive on API request field name"
)

type mockRepository struct {
	mock.Mock
}

func (m *mockRepository) List(ctx context.Context) ([]assistant.Assistant, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]assistant.Assistant), args.Error(1)
}

func (m *mockRepository) Get(ctx context.Context, id assistant.ID) (*assistant.Assistant, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*assistant.Assistant), args.Error(1)
}

func (m *mockRepository) Create(ctx context.Context, record assistant.Assistant) (assistant.ID, error) {
	args := m.Called(ctx, record)
	if args.Get(0) == nil {
		return "", args.Error(1)
	}

	return args.Get(0).(assistant.ID), args.Error(1)
}

type mockHasher struct {
	mock.Mock
}

func (m *mockHasher) Hash(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMuxWithHandler(t *testing.T, repo *mockRepository, hasher *mockHasher) *http.ServeMux {
	t.Helper()

	h, err := assistant.NewHandler(newTestLogger(), repo, hasher)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	repo := new(mockRepository)
	hasher := new(mockHasher)
	logger := newTestLogger()

	tests := []struct {
		name     string
		logger   *slog.Logger
		repo     assistant.Repository
		hasher   assistant.Hasher
		expected error
	}{
		{name: handlerCaseNilLogger, logger: nil, repo: repo, hasher: hasher, expected: assistant.ErrNilLogger},
		{name: handlerCaseNilRepository, logger: logger, repo: nil, hasher: hasher, expected: assistant.ErrNilRepository},
		{name: handlerCaseNilHasher, logger: logger, repo: repo, hasher: nil, expected: assistant.ErrNilPasswordHasher},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := assistant.NewHandler(tt.logger, tt.repo, tt.hasher)

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

		h, err := assistant.NewHandler(newTestLogger(), new(mockRepository), new(mockHasher))
		require.NoError(t, err)

		mux := http.NewServeMux()
		assert.NotPanics(t, func() {
			h.RegisterHandlers(mux)
		})
	})
}

func TestListEndpoint(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseRepositoryError, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		hasher := new(mockHasher)
		mux := newMuxWithHandler(t, repo, hasher)

		repo.On("List", mock.Anything).Return([]assistant.Assistant(nil), errors.New(handlerBoomErrMsg)).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		repo.AssertExpectations(t)
	})

	t.Run(handlerCaseSuccess, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		hasher := new(mockHasher)
		mux := newMuxWithHandler(t, repo, hasher)

		expected := []assistant.Assistant{{
			ID:           assistant.ID(handlerAssistantFixedID),
			Names:        handlerAssistantNames,
			LastNames:    handlerAssistantLastNames,
			Email:        handlerAssistantEmail,
			PasswordHash: handlerAssistantHiddenPassword,
		}}

		repo.On("List", mock.Anything).Return(expected, nil).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, handlerApplicationJSON, rec.Header().Get(handlerContentType))
		assert.Contains(t, rec.Body.String(), handlerAssistantEmail)
		assert.NotContains(t, rec.Body.String(), handlerAssistantHiddenPassword)
		repo.AssertExpectations(t)
	})
}

func TestGetEndpoint(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseInvalidID, func(t *testing.T) {
		t.Parallel()

		mux := newMuxWithHandler(t, new(mockRepository), new(mockHasher))

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+handlerInvalidAssistantID, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run(handlerCaseNotFound, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		mux := newMuxWithHandler(t, repo, new(mockHasher))

		missingID := assistant.ID(handlerAssistantFixedID)
		repo.On("Get", mock.Anything, missingID).Return((*assistant.Assistant)(nil), errors.Join(assistant.ErrAssistantNotFound, errors.New(handlerWrappedErrMsg))).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+missingID.String(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		repo.AssertExpectations(t)
	})

	t.Run(handlerCaseRepositoryError, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		mux := newMuxWithHandler(t, repo, new(mockHasher))

		recordID := assistant.ID(handlerAssistantFixedID)
		repo.On("Get", mock.Anything, recordID).Return((*assistant.Assistant)(nil), errors.New(handlerBoomErrMsg)).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+recordID.String(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		repo.AssertExpectations(t)
	})

	t.Run(handlerCaseSuccess, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		mux := newMuxWithHandler(t, repo, new(mockHasher))

		record := &assistant.Assistant{
			ID:           assistant.ID(handlerAssistantFixedID),
			Names:        handlerAssistantNames,
			LastNames:    handlerAssistantLastNames,
			Email:        handlerAssistantEmail,
			PasswordHash: handlerAssistantHiddenPassword,
		}

		repo.On("Get", mock.Anything, record.ID).Return(record, nil).Once()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, handlerAssistantsPath+"/"+record.ID.String(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, handlerApplicationJSON, rec.Header().Get(handlerContentType))
		assert.Contains(t, rec.Body.String(), handlerAssistantEmail)
		assert.NotContains(t, rec.Body.String(), handlerAssistantHiddenPassword)
		repo.AssertExpectations(t)
	})
}

func TestCreateEndpoint(t *testing.T) {
	t.Parallel()

	t.Run(handlerCaseInvalidJSON, func(t *testing.T) {
		t.Parallel()

		mux := newMuxWithHandler(t, new(mockRepository), new(mockHasher))

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString("{"))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run(handlerCaseHashingError, func(t *testing.T) {
		t.Parallel()

		hasher := new(mockHasher)
		hasher.On("Hash", handlerAssistantPlainPassword).Return("", errors.New(handlerBoomErrMsg)).Once()
		mux := newMuxWithHandler(t, new(mockRepository), hasher)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		hasher.AssertExpectations(t)
	})

	t.Run(handlerCaseValidationError, func(t *testing.T) {
		t.Parallel()

		hasher := new(mockHasher)
		hasher.On("Hash", handlerAssistantPlainPassword).Return(handlerAssistantHashedPassword, nil).Once()
		mux := newMuxWithHandler(t, new(mockRepository), hasher)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBadBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		hasher.AssertExpectations(t)
	})

	t.Run(handlerCaseRepositoryError, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		hasher := new(mockHasher)
		hasher.On("Hash", handlerAssistantPlainPassword).Return(handlerAssistantHashedPassword, nil).Once()

		repo.
			On("Create", mock.Anything, mock.MatchedBy(func(record assistant.Assistant) bool {
				return record.Names == handlerAssistantNames && record.LastNames == handlerAssistantLastNames && record.Email == handlerAssistantEmail && record.PasswordHash == handlerAssistantHashedPassword
			})).
			Return(assistant.ID(""), errors.New(handlerBoomErrMsg)).
			Once()

		mux := newMuxWithHandler(t, repo, hasher)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		repo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run(handlerCaseSuccess, func(t *testing.T) {
		t.Parallel()

		repo := new(mockRepository)
		hasher := new(mockHasher)
		hasher.On("Hash", handlerAssistantPlainPassword).Return(handlerAssistantHashedPassword, nil).Once()

		createdID := assistant.ID(handlerAssistantFixedID)
		repo.
			On("Create", mock.Anything, mock.MatchedBy(func(record assistant.Assistant) bool {
				return record.Names == handlerAssistantNames && record.LastNames == handlerAssistantLastNames && record.Email == handlerAssistantEmail && record.PasswordHash == handlerAssistantHashedPassword
			})).
			Return(createdID, nil).
			Once()

		mux := newMuxWithHandler(t, repo, hasher)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handlerAssistantsPath, bytes.NewBufferString(handlerCreateAssistantBody))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, handlerApplicationJSON, rec.Header().Get(handlerContentType))
		assert.Equal(t, handlerAssistantsPath+"/"+createdID.String(), rec.Header().Get(handlerLocationHeader))
		repo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})
}
