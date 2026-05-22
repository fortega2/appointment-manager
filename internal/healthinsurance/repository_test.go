package healthinsurance_test

import (
	"appointment-manager/internal/healthinsurance"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepositoryValidation(t *testing.T) {
	t.Parallel()

	repo, err := healthinsurance.NewRepository(nil)

	require.Error(t, err)
	assert.Nil(t, repo)
	assert.ErrorIs(t, err, healthinsurance.ErrNilPgxPool)
}
