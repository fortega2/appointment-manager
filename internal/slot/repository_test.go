package slot

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const repositoryBoomErr = "boom"

func TestNewRepositoryValidation(t *testing.T) {
	t.Parallel()

	repo, err := NewRepository(nil)

	require.Error(t, err)
	assert.Nil(t, repo)
	assert.ErrorIs(t, err, ErrNilPgxPool)
}

func TestRepositoryCreateValidation(t *testing.T) {
	t.Parallel()

	repo := &Repository{}

	err := repo.Create(t.Context(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilSlot)
}

func TestRepositoryMapCreateError(t *testing.T) {
	t.Parallel()

	repo := &Repository{}

	tests := []struct {
		name       string
		constraint string
		expected   error
	}{
		{
			name:       "fk_slot_professional constraint",
			constraint: constraintFkSlotProfessional,
			expected:   ErrInvalidProfessionalID,
		},
		{
			name:       "chk_slot_times constraint",
			constraint: constraintChkSlotTimes,
			expected:   ErrInvalidTimeRange,
		},
		{
			name:       "chk_slot_capacity constraint",
			constraint: constraintChkSlotCapacity,
			expected:   ErrInvalidMaxCapacity,
		},
		{
			name:       "chk_slot_date_consistency constraint",
			constraint: constraintChkSlotDateConsistency,
			expected:   ErrDateTimeInconsistency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := repo.mapCreateError(&pgconn.PgError{ConstraintName: tt.constraint})

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.expected)
		})
	}

	t.Run("other pg error wrapped", func(t *testing.T) {
		t.Parallel()

		original := &pgconn.PgError{ConstraintName: "other_constraint"}
		err := repo.mapCreateError(original)

		require.Error(t, err)
		assert.ErrorIs(t, err, original)
	})

	t.Run("non pg error wrapped", func(t *testing.T) {
		t.Parallel()

		original := errors.New(repositoryBoomErr)
		err := repo.mapCreateError(original)

		require.Error(t, err)
		assert.ErrorIs(t, err, original)
		assert.EqualError(t, err, "create slot: "+repositoryBoomErr)
	})
}
