package assistant

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type Repository interface {
	List(ctx context.Context) ([]Assistant, error)
	Get(ctx context.Context, id uuid.UUID) (*Assistant, error)
	Create(ctx context.Context, assistant Assistant) (uuid.UUID, error)
}

type Hasher interface {
	Hash(password string) (string, error)
}

type Service struct {
	repo   Repository
	hasher Hasher
}

func NewService(repo Repository, hasher Hasher) (*Service, error) {
	if repo == nil {
		return nil, ErrNilRepository
	}
	if hasher == nil {
		return nil, ErrNilPasswordHasher
	}

	return &Service{
		repo:   repo,
		hasher: hasher,
	}, nil
}

func (s *Service) List(ctx context.Context) ([]Assistant, error) {
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Assistant, error) {
	return s.repo.Get(ctx, id)
}

type CreateInput struct {
	FirstName string
	LastName  string
	Email     string
	Password  string //nolint:gosec // Service input requires explicit password field.
}

func (s *Service) Create(ctx context.Context, input CreateInput) (uuid.UUID, error) {
	if err := validateCreateInput(input); err != nil {
		return uuid.Nil, err
	}

	hashedPassword, err := s.hasher.Hash(input.Password)
	if err != nil {
		return uuid.Nil, err
	}
	if strings.TrimSpace(hashedPassword) == "" {
		return uuid.Nil, ErrEmptyPasswordHash
	}

	assist, err := NewAssistant(input.FirstName, input.LastName, input.Email, hashedPassword)
	if err != nil {
		return uuid.Nil, err
	}

	return s.repo.Create(ctx, *assist)
}

func validateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.FirstName) == "" {
		return ErrAssistantRequestFirstNameRequired
	}
	if strings.TrimSpace(input.LastName) == "" {
		return ErrAssistantRequestLastNameRequired
	}
	if strings.TrimSpace(input.Email) == "" {
		return ErrAssistantRequestEmailRequired
	}
	if strings.TrimSpace(input.Password) == "" {
		return ErrAssistantRequestPasswordRequired
	}

	return nil
}
