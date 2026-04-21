package session

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromContextSuccess(t *testing.T) {
	t.Parallel()

	expected := &Session{UserID: "assistant-1", Email: "assistant@email.com"}
	ctx := context.WithValue(t.Context(), SessionKey, expected)

	actual, err := FromContext(ctx)
	require.NoError(t, err)
	require.NotNil(t, actual)
	assert.Equal(t, expected, actual)
}

func TestFromContextMissing(t *testing.T) {
	t.Parallel()

	actual, err := FromContext(t.Context())
	require.Error(t, err)
	assert.Nil(t, actual)
	assert.True(t, errors.Is(err, ErrSessionNotInContext))
}

func TestFromContextWrongType(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(t.Context(), SessionKey, "wrong")

	actual, err := FromContext(ctx)
	require.Error(t, err)
	assert.Nil(t, actual)
	assert.True(t, errors.Is(err, ErrSessionNotInContext))
}
