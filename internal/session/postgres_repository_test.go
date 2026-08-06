package session

import (
	"appointment-manager/internal/domain"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const repositoryErrMsg = "test: %w"

func TestNewPostgresRepositoryValidation(t *testing.T) {
	t.Parallel()

	repo, err := NewPostgresRepository(nil)
	require.Error(t, err)
	assert.Nil(t, repo)
	assert.ErrorIs(t, err, ErrNilPgxPool)
}

func TestPostgresRepositoryCreateInvalidAssistantID(t *testing.T) {
	t.Parallel()

	repo := &PostgresRepository{}

	err := repo.Create(t.Context(), Session{UserID: "not-a-uuid"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAssistantID)
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestPostgresRepositoryCreateEmptyAssistantID(t *testing.T) {
	t.Parallel()

	repo := &PostgresRepository{}

	err := repo.Create(t.Context(), Session{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAssistantID)
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestMapPgxError(t *testing.T) {
	t.Parallel()

	otherErr := errors.New("boom")

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{
			name:     "unknown assistant",
			err:      &pgconn.PgError{ConstraintName: constraintFkSessionAssistant},
			expected: ErrUnknownAssistant,
		},
		{
			name:     "other constraint",
			err:      &pgconn.PgError{ConstraintName: "some_other_constraint"},
			expected: nil,
		},
		{
			name:     "not a pg error",
			err:      otherErr,
			expected: otherErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := mapPgxError(repositoryErrMsg, tt.err)
			require.Error(t, err)

			if tt.expected != nil {
				assert.ErrorIs(t, err, tt.expected)
				return
			}

			assert.NotErrorIs(t, err, ErrUnknownAssistant)
		})
	}
}
