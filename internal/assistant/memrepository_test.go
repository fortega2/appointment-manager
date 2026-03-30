package assistant_test

import (
	"appointment-manager/internal/assistant"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	memAssistantNames          = "Jane"
	memAssistantLastNames      = "Doe"
	memAssistantEmail          = "jane.doe@email.com"
	memAssistantHashedPassword = "hashed"
	memAssistantSeedNames      = "John"
	memAssistantSeedEmail      = "fakeemail@email.com"
	memAssistantSeedPassword   = "password123"
	memMissingIDLiteral        = "11111111-1111-1111-1111-111111111111"
	memAssistantMutatedNames   = "Mutated"
	memNameFmt                 = "Name-%d"
	memPersonEmailFmt          = "person-%d@email.com"
)

func TestNewMemRepository(t *testing.T) {
	t.Parallel()

	repo := assistant.NewMemRepository()

	assistants, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, assistants, 1)
	assert.NotEmpty(t, assistants[0].ID)
	assert.Equal(t, memAssistantSeedNames, assistants[0].Names)
	assert.Equal(t, memAssistantLastNames, assistants[0].LastNames)
	assert.Equal(t, memAssistantSeedEmail, assistants[0].Email)
	assert.Equal(t, memAssistantSeedPassword, assistants[0].PasswordHash)
}

func TestMemRepositoryCreateAndGet(t *testing.T) {
	t.Parallel()

	repo := assistant.NewMemRepository()
	ctx := context.Background()

	id, err := repo.Create(ctx, assistant.Assistant{
		Names:        memAssistantNames,
		LastNames:    memAssistantLastNames,
		Email:        memAssistantEmail,
		PasswordHash: memAssistantHashedPassword,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, id)

	assistantRecord, getErr := repo.Get(ctx, id)
	require.NoError(t, getErr)
	require.NotNil(t, assistantRecord)
	assert.Equal(t, id, assistantRecord.ID)
	assert.Equal(t, memAssistantNames, assistantRecord.Names)
	assert.Equal(t, memAssistantLastNames, assistantRecord.LastNames)
	assert.Equal(t, memAssistantEmail, assistantRecord.Email)
	assert.Equal(t, memAssistantHashedPassword, assistantRecord.PasswordHash)
}

func TestMemRepositoryGetNotFound(t *testing.T) {
	t.Parallel()

	repo := assistant.NewMemRepository()

	missingID := uuid.MustParse(memMissingIDLiteral)
	assistantRecord, err := repo.Get(context.Background(), missingID)

	require.Error(t, err)
	assert.Nil(t, assistantRecord)
	assert.True(t, errors.Is(err, assistant.ErrAssistantNotFound))
}

func TestMemRepositoryGetReturnsCopy(t *testing.T) {
	t.Parallel()

	repo := assistant.NewMemRepository()
	ctx := context.Background()

	assistants, err := repo.List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, assistants)

	seedID := assistants[0].ID

	firstRead, firstErr := repo.Get(ctx, seedID)
	require.NoError(t, firstErr)
	firstRead.Names = memAssistantMutatedNames

	secondRead, secondErr := repo.Get(ctx, seedID)
	require.NoError(t, secondErr)
	assert.Equal(t, memAssistantSeedNames, secondRead.Names)
}

func TestMemRepositoryConcurrentAccess(t *testing.T) {
	t.Parallel()

	repo := assistant.NewMemRepository()
	ctx := context.Background()

	assistants, err := repo.List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, assistants)

	seedID := assistants[0].ID

	const workers = 100
	var wg sync.WaitGroup
	errCh := make(chan error, workers*3)

	for i := range workers {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			if _, listErr := repo.List(ctx); listErr != nil {
				errCh <- listErr
			}

			if _, getErr := repo.Get(ctx, seedID); getErr != nil {
				errCh <- getErr
			}

			if _, createErr := repo.Create(ctx, assistant.Assistant{
				Names:        fmt.Sprintf(memNameFmt, i),
				LastNames:    memAssistantLastNames,
				Email:        fmt.Sprintf(memPersonEmailFmt, i),
				PasswordHash: memAssistantHashedPassword,
			}); createErr != nil {
				errCh <- createErr
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for goroutineErr := range errCh {
		require.NoError(t, goroutineErr)
	}

	finalAssistants, finalErr := repo.List(ctx)
	require.NoError(t, finalErr)
	assert.GreaterOrEqual(t, len(finalAssistants), workers+1)
}
