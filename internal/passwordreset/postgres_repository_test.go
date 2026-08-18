package passwordreset

import (
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

func TestMapPgxError(t *testing.T) {
	t.Parallel()

	otherErr := errors.New("boom")

	tests := []struct {
		err      error
		expected error
		name     string
	}{
		{
			name:     "unknown assistant",
			err:      &pgconn.PgError{ConstraintName: constraintFkPasswordResetTokenAssistant},
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
