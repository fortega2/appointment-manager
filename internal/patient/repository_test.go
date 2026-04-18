package patient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.ErrorIs(t, err, ErrNilPatient)
}
