package prescription

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQueryValidation(t *testing.T) {
	t.Parallel()

	q, err := NewQuery(nil)

	require.Error(t, err)
	assert.Nil(t, q)
	assert.ErrorIs(t, err, ErrNilPgxPool)
}
