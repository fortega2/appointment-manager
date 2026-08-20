package assistant_test

import (
	"appointment-manager/internal/assistant"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPostgresRepositoryValidation(t *testing.T) {
	t.Parallel()

	repo, err := assistant.NewPostgresRepository(nil)
	require.Error(t, err)
	assert.Nil(t, repo)
	assert.ErrorIs(t, err, assistant.ErrNilPgxPool)
}

// TestPostgresRepositoryUpdatePasswordHashRejectsBlank covers the guard that
// runs before the pool is touched: writing a blank hash would lock the account
// out for good, since no password can verify against it.
func TestPostgresRepositoryUpdatePasswordHashRejectsBlank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hash string
	}{
		{name: "empty", hash: ""},
		{name: "blank", hash: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &assistant.PostgresRepository{}

			err := repo.UpdatePasswordHash(t.Context(), uuid.Must(uuid.NewV7()), tt.hash)
			require.Error(t, err)
			assert.ErrorIs(t, err, assistant.ErrEmptyPasswordHash)
		})
	}
}
