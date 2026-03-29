package assistant

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type MemRepository struct {
	store map[ID]*Assistant
}

func NewMemRepository() *MemRepository {
	store := make(map[ID]*Assistant)
	ID := ID(uuid.NewString())
	store[ID] = &Assistant{
		ID:           ID,
		Names:        "John",
		LastNames:    "Doe",
		Email:        "fakeemail@email.com",
		PasswordHash: "password123",
	}

	return &MemRepository{
		store: store,
	}
}

func (r *MemRepository) List(_ context.Context) ([]Assistant, error) {
	assistants := make([]Assistant, 0, len(r.store))
	for _, assistant := range r.store {
		assistants = append(assistants, *assistant)
	}
	return assistants, nil
}

func (r *MemRepository) Get(_ context.Context, id ID) (*Assistant, error) {
	assistant, ok := r.store[id]
	if !ok {
		return nil, fmt.Errorf("%w: ID: %s", ErrAssistantNotFound, id)
	}
	return assistant, nil
}

func (r *MemRepository) Create(_ context.Context, assistant Assistant) (ID, error) {
	id := ID(uuid.NewString())
	assistant.ID = id
	r.store[id] = &assistant
	return id, nil
}
