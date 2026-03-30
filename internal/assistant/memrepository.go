package assistant

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type MemRepository struct {
	mu    sync.RWMutex
	store map[uuid.UUID]*Assistant
}

func NewMemRepository() *MemRepository {
	store := make(map[uuid.UUID]*Assistant)
	seedID := uuid.New()
	store[seedID] = &Assistant{
		ID:           seedID,
		FirstName:    "John",
		LastName:     "Doe",
		Email:        "fakeemail@email.com",
		PasswordHash: "password123",
	}

	return &MemRepository{
		store: store,
	}
}

func (r *MemRepository) List(_ context.Context) ([]Assistant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	assistants := make([]Assistant, 0, len(r.store))
	for _, assistant := range r.store {
		assistants = append(assistants, *assistant)
	}
	return assistants, nil
}

func (r *MemRepository) Get(_ context.Context, id uuid.UUID) (*Assistant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	assistant, ok := r.store[id]
	if !ok {
		return nil, fmt.Errorf("%w: ID: %s", ErrAssistantNotFound, id)
	}

	assistantCopy := *assistant
	return &assistantCopy, nil
}

func (r *MemRepository) Create(_ context.Context, assistant Assistant) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := uuid.New()
	assistant.ID = id
	r.store[id] = &assistant
	return id, nil
}
