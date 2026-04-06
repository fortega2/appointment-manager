package appointment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const serviceBoomError = "boom"

type serviceRepoMock struct {
	mock.Mock
}

func (m *serviceRepoMock) List(ctx context.Context, filter ListFilter) ([]Appointment, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]Appointment), args.Error(1)
}

func (m *serviceRepoMock) Create(ctx context.Context, appoint Appointment) (uuid.UUID, error) {
	args := m.Called(ctx, appoint)
	if args.Get(0) == nil {
		return uuid.Nil, args.Error(1)
	}

	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *serviceRepoMock) GetWindow(ctx context.Context, appointmentID uuid.UUID) (Window, error) {
	args := m.Called(ctx, appointmentID)
	if args.Get(0) == nil {
		return Window{}, args.Error(1)
	}

	return args.Get(0).(Window), args.Error(1)
}

func (m *serviceRepoMock) UpdateStatus(ctx context.Context, appointmentID uuid.UUID, newStatus, expectedStatus Status) error {
	args := m.Called(ctx, appointmentID, newStatus, expectedStatus)
	return args.Error(0)
}

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()

	svc, err := NewService(nil)

	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, ErrNilRepository)
}

func TestServiceListValidation(t *testing.T) {
	t.Parallel()

	repo := new(serviceRepoMock)
	svc, err := NewService(repo)
	require.NoError(t, err)

	_, err = svc.List(context.Background(), ListInput{Page: "0"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPage)
	repo.AssertNotCalled(t, "List", mock.Anything, mock.Anything)

	_, err = svc.List(context.Background(), ListInput{Limit: "101"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidLimit)
	repo.AssertNotCalled(t, "List", mock.Anything, mock.Anything)

	_, err = svc.List(context.Background(), ListInput{Status: "99"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidStatus)
	repo.AssertNotCalled(t, "List", mock.Anything, mock.Anything)
}

func TestServiceListSuccess(t *testing.T) {
	t.Parallel()

	repo := new(serviceRepoMock)
	svc, err := NewService(repo)
	require.NoError(t, err)

	expected := []Appointment{{ID: uuid.New()}}
	repo.On("List", mock.Anything, ListFilter{Page: 1, Limit: 20, Status: StatusConfirmed}).Return(expected, nil).Once()

	result, listErr := svc.List(context.Background(), ListInput{})

	require.NoError(t, listErr)
	assert.Equal(t, expected, result)
	repo.AssertExpectations(t)
}

func TestServiceCreateValidation(t *testing.T) {
	t.Parallel()

	repo := new(serviceRepoMock)
	svc, err := NewService(repo)
	require.NoError(t, err)

	_, createErr := svc.Create(context.Background(), CreateInput{})
	require.Error(t, createErr)
	assert.ErrorIs(t, createErr, ErrSlotIDRequired)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)

	_, createErr = svc.Create(context.Background(), CreateInput{SlotID: "invalid"})
	require.Error(t, createErr)
	assert.ErrorIs(t, createErr, ErrInvalidSlotID)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestServiceCreateSuccess(t *testing.T) {
	t.Parallel()

	repo := new(serviceRepoMock)
	svc, err := NewService(repo)
	require.NoError(t, err)

	slotID := uuid.New()
	patientID := uuid.New()
	professionalID := uuid.New()
	assistantID := uuid.New()
	notes := " follow-up "
	createdID := uuid.New()

	repo.On("Create", mock.Anything, mock.MatchedBy(func(appoint Appointment) bool {
		return appoint.SlotID == slotID &&
			appoint.PatientID == patientID &&
			appoint.ProfessionalID == professionalID &&
			appoint.AssistantID == assistantID &&
			appoint.Status == StatusConfirmed &&
			appoint.Notes == &notes
	})).Return(createdID, nil).Once()

	id, createErr := svc.Create(context.Background(), CreateInput{
		SlotID:         slotID.String(),
		PatientID:      patientID.String(),
		ProfessionalID: professionalID.String(),
		AssistantID:    assistantID.String(),
		Notes:          &notes,
	})

	require.NoError(t, createErr)
	assert.Equal(t, createdID, id)
	repo.AssertExpectations(t)
}

func TestServiceCancel(t *testing.T) {
	t.Parallel()

	appointmentID := uuid.New()
	referenceTime := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)

	t.Run("confirmed before 24h becomes cancelled", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(25 * time.Hour),
			EndTime:   referenceTime.Add(26 * time.Hour),
			Status:    StatusConfirmed,
		}, nil).Once()
		repo.On("UpdateStatus", mock.Anything, appointmentID, StatusCancelled, StatusConfirmed).Return(nil).Once()

		err = svc.Cancel(context.Background(), appointmentID)

		require.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("confirmed inside 24h becomes absent", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(2 * time.Hour),
			EndTime:   referenceTime.Add(3 * time.Hour),
			Status:    StatusConfirmed,
		}, nil).Once()
		repo.On("UpdateStatus", mock.Anything, appointmentID, StatusAbsent, StatusConfirmed).Return(nil).Once()

		err = svc.Cancel(context.Background(), appointmentID)

		require.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("confirmed after start becomes absent", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(-30 * time.Minute),
			EndTime:   referenceTime.Add(30 * time.Minute),
			Status:    StatusConfirmed,
		}, nil).Once()
		repo.On("UpdateStatus", mock.Anything, appointmentID, StatusAbsent, StatusConfirmed).Return(nil).Once()

		err = svc.Cancel(context.Background(), appointmentID)

		require.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("already cancelled is idempotent", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(1 * time.Hour),
			EndTime:   referenceTime.Add(2 * time.Hour),
			Status:    StatusCancelled,
		}, nil).Once()

		err = svc.Cancel(context.Background(), appointmentID)

		require.NoError(t, err)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})

	t.Run("invalid status for cancel returns conflict", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(1 * time.Hour),
			EndTime:   referenceTime.Add(2 * time.Hour),
			Status:    StatusAttended,
		}, nil).Once()

		err = svc.Cancel(context.Background(), appointmentID)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAppointmentCannotCancelWithStatus)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})
}

func TestServiceAttend(t *testing.T) {
	t.Parallel()

	appointmentID := uuid.New()
	referenceTime := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)

	t.Run("confirmed in slot range becomes attended", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(-30 * time.Minute),
			EndTime:   referenceTime.Add(30 * time.Minute),
			Status:    StatusConfirmed,
		}, nil).Once()
		repo.On("UpdateStatus", mock.Anything, appointmentID, StatusAttended, StatusConfirmed).Return(nil).Once()

		err = svc.Attend(context.Background(), appointmentID)

		require.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("attend before start returns validation", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(1 * time.Hour),
			EndTime:   referenceTime.Add(2 * time.Hour),
			Status:    StatusConfirmed,
		}, nil).Once()

		err = svc.Attend(context.Background(), appointmentID)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAppointmentCannotAttendNow)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})

	t.Run("attend after end returns validation", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(-2 * time.Hour),
			EndTime:   referenceTime.Add(-1 * time.Hour),
			Status:    StatusConfirmed,
		}, nil).Once()

		err = svc.Attend(context.Background(), appointmentID)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAppointmentCannotAttendNow)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})

	t.Run("already attended is idempotent", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(-30 * time.Minute),
			EndTime:   referenceTime.Add(30 * time.Minute),
			Status:    StatusAttended,
		}, nil).Once()

		err = svc.Attend(context.Background(), appointmentID)

		require.NoError(t, err)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})

	t.Run("invalid status for attend returns conflict", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(-30 * time.Minute),
			EndTime:   referenceTime.Add(30 * time.Minute),
			Status:    StatusCancelled,
		}, nil).Once()

		err = svc.Attend(context.Background(), appointmentID)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAppointmentCannotAttendWithStatus)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})
}

func TestServiceActionRepositoryError(t *testing.T) {
	t.Parallel()

	referenceTime := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	appointmentID := uuid.New()

	t.Run("cancel propagates update status error", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(2 * time.Hour),
			EndTime:   referenceTime.Add(3 * time.Hour),
			Status:    StatusConfirmed,
		}, nil).Once()
		repo.On("UpdateStatus", mock.Anything, appointmentID, StatusAbsent, StatusConfirmed).Return(errors.New(serviceBoomError)).Once()

		err = svc.Cancel(context.Background(), appointmentID)

		require.Error(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("attend propagates update status error", func(t *testing.T) {
		t.Parallel()

		repo := new(serviceRepoMock)
		svc, err := newServiceWithClock(repo, func() time.Time { return referenceTime })
		require.NoError(t, err)

		repo.On("GetWindow", mock.Anything, appointmentID).Return(Window{
			StartTime: referenceTime.Add(-30 * time.Minute),
			EndTime:   referenceTime.Add(30 * time.Minute),
			Status:    StatusConfirmed,
		}, nil).Once()
		repo.On("UpdateStatus", mock.Anything, appointmentID, StatusAttended, StatusConfirmed).Return(errors.New(serviceBoomError)).Once()

		err = svc.Attend(context.Background(), appointmentID)

		require.Error(t, err)
		repo.AssertExpectations(t)
	})
}
