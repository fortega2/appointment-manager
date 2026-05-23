package slot_test

import (
	"appointment-manager/internal/slot"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	professionalID   = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	slotDate         = "2026-05-25"
	slotStartTime    = "2026-05-25T10:00:00Z"
	slotEndTime      = "2026-05-25T11:00:00Z"
	slotMaxCapacity  = int16(5)
	wrongDateForSlot = "2026-05-26"
)

func TestNewSlot(t *testing.T) {
	t.Parallel()

	parsedProfessionalID := uuid.MustParse(professionalID)
	parsedDate := mustParseTime(slotDate)
	parsedStartTime := mustParseTime(slotStartTime)
	parsedEndTime := mustParseTime(slotEndTime)

	created, err := slot.NewSlot(parsedProfessionalID, parsedDate, parsedStartTime, parsedEndTime, slotMaxCapacity)

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, parsedProfessionalID, created.ProfessionalID)
	assert.Equal(t, parsedDate, created.Date)
	assert.True(t, parsedStartTime.Equal(created.StartTime))
	assert.True(t, parsedEndTime.Equal(created.EndTime))
	assert.Equal(t, slotMaxCapacity, created.MaxCapacity)
	assert.False(t, created.Blocked)
}

func TestNewSlotValidation(t *testing.T) {
	t.Parallel()

	parsedProfessionalID := uuid.MustParse(professionalID)
	parsedDate := mustParseTime(slotDate)
	parsedStartTime := mustParseTime(slotStartTime)
	parsedEndTime := mustParseTime(slotEndTime)

	tests := []struct {
		name          string
		professional  uuid.UUID
		date          time.Time
		startTime     time.Time
		endTime       time.Time
		maxCapacity   int16
		expectedError error
	}{
		{
			name:          "professional ID is nil",
			professional:  uuid.Nil,
			date:          parsedDate,
			startTime:     parsedStartTime,
			endTime:       parsedEndTime,
			maxCapacity:   slotMaxCapacity,
			expectedError: slot.ErrInvalidProfessionalID,
		},
		{
			name:          "end time before start",
			professional:  parsedProfessionalID,
			date:          parsedDate,
			startTime:     parsedStartTime,
			endTime:       mustParseTime("2026-05-25T09:00:00Z"),
			maxCapacity:   slotMaxCapacity,
			expectedError: slot.ErrInvalidTimeRange,
		},
		{
			name:          "end time equals start",
			professional:  parsedProfessionalID,
			date:          parsedDate,
			startTime:     parsedStartTime,
			endTime:       parsedStartTime,
			maxCapacity:   slotMaxCapacity,
			expectedError: slot.ErrInvalidTimeRange,
		},
		{
			name:          "max capacity is zero",
			professional:  parsedProfessionalID,
			date:          parsedDate,
			startTime:     parsedStartTime,
			endTime:       parsedEndTime,
			maxCapacity:   0,
			expectedError: slot.ErrInvalidMaxCapacity,
		},
		{
			name:          "max capacity is negative",
			professional:  parsedProfessionalID,
			date:          parsedDate,
			startTime:     parsedStartTime,
			endTime:       parsedEndTime,
			maxCapacity:   -1,
			expectedError: slot.ErrInvalidMaxCapacity,
		},
		{
			name:          "date is zero",
			professional:  parsedProfessionalID,
			date:          time.Time{},
			startTime:     parsedStartTime,
			endTime:       parsedEndTime,
			maxCapacity:   slotMaxCapacity,
			expectedError: slot.ErrInvalidDate,
		},
		{
			name:          "date does not match start time",
			professional:  parsedProfessionalID,
			date:          mustParseTime(wrongDateForSlot),
			startTime:     parsedStartTime,
			endTime:       parsedEndTime,
			maxCapacity:   slotMaxCapacity,
			expectedError: slot.ErrDateTimeInconsistency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := slot.NewSlot(tt.professional, tt.date, tt.startTime, tt.endTime, tt.maxCapacity)

			require.Error(t, err)
			assert.Nil(t, created)
			assert.True(t, errors.Is(err, tt.expectedError))
		})
	}
}

func mustParseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t, err = time.Parse("2006-01-02", value)
		if err != nil {
			panic("failed to parse test time: " + value)
		}
	}

	return t
}
