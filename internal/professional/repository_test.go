package professional

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
	assert.ErrorIs(t, err, ErrNilProfessional)
}

func TestRepositoryMapCreateError(t *testing.T) {
	t.Parallel()

	repo := &Repository{}

	t.Run("mapped specialty constraint", func(t *testing.T) {
		t.Parallel()

		err := repo.mapCreateError(&pgconn.PgError{ConstraintName: constraintCheckProfessionalSpecialty})

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidProfessionalSpecialty)
	})

	t.Run("other pg error wrapped", func(t *testing.T) {
		t.Parallel()

		original := &pgconn.PgError{ConstraintName: "other_constraint"}
		err := repo.mapCreateError(original)

		require.Error(t, err)
		assert.ErrorIs(t, err, original)
		assert.Contains(t, err.Error(), "create professional:")
	})

	t.Run("non pg error wrapped", func(t *testing.T) {
		t.Parallel()

		original := errors.New(repositoryBoomErr)
		err := repo.mapCreateError(original)

		require.Error(t, err)
		assert.ErrorIs(t, err, original)
		assert.EqualError(t, err, "create professional: "+repositoryBoomErr)
	})
}
