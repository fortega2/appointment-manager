package assistant_test

import (
	"appointment-manager/internal/assistant"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	serviceAssistantNames      = "Jane"
	serviceAssistantLastNames  = "Doe"
	serviceAssistantEmail      = "jane.doe@email.com"
	serviceAssistantPassword   = "123456"
	serviceAssistantHash       = "hashed-password"
	serviceAssistantFixedID    = "a6f3d3f9-6dd2-4848-8af9-98fe0c060816"
	serviceAssistantMissingID  = "11111111-1111-1111-1111-111111111111"
	serviceBoomErrMsg          = "boom"
	serviceCaseNilRepository   = "nil repository"
	serviceCaseNilHasher       = "nil hasher"
	serviceCaseRepositoryError = "repository error"
	serviceCaseHasherError     = "hasher error"
	serviceCaseEmptyHashError  = "empty hash error"
	serviceCaseValidationError = "validation error"
	serviceCaseSuccess         = "success"
	emptyValue                 = ""
)

type serviceRepoMock struct {
	mock.Mock
}

func (m *serviceRepoMock) List(ctx context.Context) ([]assistant.Assistant, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]assistant.Assistant), args.Error(1)
}

func (m *serviceRepoMock) Get(ctx context.Context, id assistant.ID) (*assistant.Assistant, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*assistant.Assistant), args.Error(1)
}

func (m *serviceRepoMock) Create(ctx context.Context, record assistant.Assistant) (assistant.ID, error) {
	args := m.Called(ctx, record)
	if args.Get(0) == nil {
		return "", args.Error(1)
	}

	return args.Get(0).(assistant.ID), args.Error(1)
}

type serviceHasherMock struct {
	mock.Mock
}

func (m *serviceHasherMock) Hash(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()

	repo := new(serviceRepoMock)
	hasher := new(serviceHasherMock)

	tests := []struct {
		name     string
		repo     assistant.Repository
		hasher   assistant.Hasher
		expected error
	}{
		{name: serviceCaseNilRepository, repo: nil, hasher: hasher, expected: assistant.ErrNilRepository},
		{name: serviceCaseNilHasher, repo: repo, hasher: nil, expected: assistant.ErrNilPasswordHasher},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := assistant.NewService(tt.repo, tt.hasher)

			require.Error(t, err)
			assert.Nil(t, svc)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestServiceList(t *testing.T) {
	t.Parallel()

	t.Run(serviceCaseRepositoryError, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		repo.On("List", mock.Anything).Return([]assistant.Assistant(nil), errors.New(serviceBoomErrMsg)).Once()

		result, listErr := svc.List(context.Background())

		require.Error(t, listErr)
		assert.Nil(t, result)
		repo.AssertExpectations(t)
	})

	t.Run(serviceCaseSuccess, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		expected := []assistant.Assistant{{
			ID:           assistant.ID(serviceAssistantFixedID),
			Names:        serviceAssistantNames,
			LastNames:    serviceAssistantLastNames,
			Email:        serviceAssistantEmail,
			PasswordHash: serviceAssistantHash,
		}}

		repo.On("List", mock.Anything).Return(expected, nil).Once()

		result, listErr := svc.List(context.Background())

		require.NoError(t, listErr)
		assert.Equal(t, expected, result)
		repo.AssertExpectations(t)
	})
}

func TestServiceGet(t *testing.T) {
	t.Parallel()

	t.Run(serviceCaseRepositoryError, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		requestedID := assistant.ID(serviceAssistantMissingID)
		repo.On("Get", mock.Anything, requestedID).Return((*assistant.Assistant)(nil), errors.New(serviceBoomErrMsg)).Once()

		result, getErr := svc.Get(context.Background(), requestedID)

		require.Error(t, getErr)
		assert.Nil(t, result)
		repo.AssertExpectations(t)
	})

	t.Run(serviceCaseSuccess, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		record := &assistant.Assistant{
			ID:           assistant.ID(serviceAssistantFixedID),
			Names:        serviceAssistantNames,
			LastNames:    serviceAssistantLastNames,
			Email:        serviceAssistantEmail,
			PasswordHash: serviceAssistantHash,
		}

		repo.On("Get", mock.Anything, record.ID).Return(record, nil).Once()

		result, getErr := svc.Get(context.Background(), record.ID)

		require.NoError(t, getErr)
		assert.Equal(t, record, result)
		repo.AssertExpectations(t)
	})
}

func TestServiceCreate(t *testing.T) {
	t.Parallel()

	t.Run("password required", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		id, createErr := svc.Create(context.Background(), assistant.CreateInput{
			Names:     serviceAssistantNames,
			LastNames: serviceAssistantLastNames,
			Email:     serviceAssistantEmail,
			Password:  "   ",
		})

		require.Error(t, createErr)
		assert.Empty(t, id)
		assert.True(t, errors.Is(createErr, assistant.ErrAssistantRequestPasswordRequired))
		hasher.AssertNotCalled(t, "Hash", mock.Anything)
		repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	})

	t.Run(serviceCaseHasherError, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		hasher.On("Hash", serviceAssistantPassword).Return(emptyValue, errors.New(serviceBoomErrMsg)).Once()

		id, createErr := svc.Create(context.Background(), assistant.CreateInput{
			Names:     serviceAssistantNames,
			LastNames: serviceAssistantLastNames,
			Email:     serviceAssistantEmail,
			Password:  serviceAssistantPassword,
		})

		require.Error(t, createErr)
		assert.Empty(t, id)
		hasher.AssertExpectations(t)
		repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	})

	t.Run(serviceCaseEmptyHashError, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		hasher.On("Hash", serviceAssistantPassword).Return("", nil).Once()

		id, createErr := svc.Create(context.Background(), assistant.CreateInput{
			Names:     serviceAssistantNames,
			LastNames: serviceAssistantLastNames,
			Email:     serviceAssistantEmail,
			Password:  serviceAssistantPassword,
		})

		require.Error(t, createErr)
		assert.Empty(t, id)
		assert.True(t, errors.Is(createErr, assistant.ErrEmptyPasswordHash))
		hasher.AssertExpectations(t)
		repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	})

	t.Run(serviceCaseValidationError, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		id, createErr := svc.Create(context.Background(), assistant.CreateInput{
			Names:     "",
			LastNames: serviceAssistantLastNames,
			Email:     serviceAssistantEmail,
			Password:  serviceAssistantPassword,
		})

		require.Error(t, createErr)
		assert.Empty(t, id)
		assert.True(t, errors.Is(createErr, assistant.ErrAssistantRequestNamesRequired))
		hasher.AssertNotCalled(t, "Hash", mock.Anything)
		repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	})

	t.Run(serviceCaseRepositoryError, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		hasher.On("Hash", serviceAssistantPassword).Return(serviceAssistantHash, nil).Once()
		repo.On("Create", mock.Anything, mock.MatchedBy(func(record assistant.Assistant) bool {
			return record.Names == serviceAssistantNames &&
				record.LastNames == serviceAssistantLastNames &&
				record.Email == serviceAssistantEmail &&
				record.PasswordHash == serviceAssistantHash
		})).Return(assistant.ID(""), errors.New(serviceBoomErrMsg)).Once()

		id, createErr := svc.Create(context.Background(), assistant.CreateInput{
			Names:     serviceAssistantNames,
			LastNames: serviceAssistantLastNames,
			Email:     serviceAssistantEmail,
			Password:  serviceAssistantPassword,
		})

		require.Error(t, createErr)
		assert.Empty(t, id)
		hasher.AssertExpectations(t)
		repo.AssertExpectations(t)
	})

	t.Run(serviceCaseSuccess, func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		hasher := new(serviceHasherMock)
		svc, err := assistant.NewService(repo, hasher)
		require.NoError(t, err)

		createdID := assistant.ID(serviceAssistantFixedID)
		hasher.On("Hash", serviceAssistantPassword).Return(serviceAssistantHash, nil).Once()
		repo.On("Create", mock.Anything, mock.MatchedBy(func(record assistant.Assistant) bool {
			return record.Names == serviceAssistantNames &&
				record.LastNames == serviceAssistantLastNames &&
				record.Email == serviceAssistantEmail &&
				record.PasswordHash == serviceAssistantHash
		})).Return(createdID, nil).Once()

		id, createErr := svc.Create(context.Background(), assistant.CreateInput{
			Names:     serviceAssistantNames,
			LastNames: serviceAssistantLastNames,
			Email:     serviceAssistantEmail,
			Password:  serviceAssistantPassword,
		})

		require.NoError(t, createErr)
		assert.Equal(t, createdID, id)
		hasher.AssertExpectations(t)
		repo.AssertExpectations(t)
	})
}
