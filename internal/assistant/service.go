package assistant

import (
	"context"
	"strings"
)

type Repository interface {
	List(ctx context.Context) ([]Assistant, error)
	Get(ctx context.Context, id ID) (*Assistant, error)
	Create(ctx context.Context, assistant Assistant) (ID, error)
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

func (s *Service) Get(ctx context.Context, id ID) (*Assistant, error) {
	return s.repo.Get(ctx, id)
}

type CreateInput struct {
	Names     string
	LastNames string
	Email     string
	Password  string //nolint:gosec // Service input requires explicit password field.
}

func (s *Service) Create(ctx context.Context, input CreateInput) (ID, error) {
	if err := validateCreateInput(input); err != nil {
		return "", err
	}

	hashedPassword, err := s.hasher.Hash(input.Password)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(hashedPassword) == "" {
		return "", ErrEmptyPasswordHash
	}

	assist, err := NewAssistant(input.Names, input.LastNames, input.Email, hashedPassword)
	if err != nil {
		return "", err
	}

	return s.repo.Create(ctx, *assist)
}

func validateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.Names) == "" {
		return ErrAssistantRequestNamesRequired
	}
	if strings.TrimSpace(input.LastNames) == "" {
		return ErrAssistantRequestLastNamesRequired
	}
	if strings.TrimSpace(input.Email) == "" {
		return ErrAssistantRequestEmailRequired
	}
	if strings.TrimSpace(input.Password) == "" {
		return ErrAssistantRequestPasswordRequired
	}

	return nil
}
