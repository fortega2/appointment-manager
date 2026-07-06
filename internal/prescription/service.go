package prescription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"appointment-manager/internal/domain"
	"appointment-manager/internal/storage"

	"github.com/google/uuid"
)

const sniffLen = 512

var extensionByContentType = map[string]string{
	"application/pdf": ".pdf",
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
}

type Service struct {
	repo    *Repository
	storage *storage.Client
}

func NewService(repo *Repository, storageClient *storage.Client) (*Service, error) {
	if repo == nil {
		return nil, ErrNilRepository
	}
	if storageClient == nil {
		return nil, ErrNilStorageClient
	}

	return &Service{repo: repo, storage: storageClient}, nil
}

func (s *Service) Create(ctx context.Context, patientID uuid.UUID, totalSessions int, file multipart.File, header *multipart.FileHeader) (*Prescription, error) {
	if file == nil {
		return nil, ErrNilFile
	}
	if header == nil {
		return nil, ErrNilFileHeader
	}

	ext, contentType, err := detectFileType(file)
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("%s/%s%s", patientID, domain.NewID(), ext)
	if err := s.storage.Upload(ctx, key, file, header.Size, contentType); err != nil {
		return nil, fmt.Errorf("upload prescription document: %w", err)
	}

	p, err := New(patientID, key, totalSessions)
	if err != nil {
		_ = s.storage.Remove(ctx, key)
		return nil, err
	}

	if err := s.repo.Create(ctx, p); err != nil {
		_ = s.storage.Remove(ctx, key)
		return nil, err
	}

	return p, nil
}

func (s *Service) Cancel(ctx context.Context, id uuid.UUID) error {
	return s.repo.UpdateStatus(ctx, id, StatusCancelled)
}

func (s *Service) PresignedGetURL(ctx context.Context, id uuid.UUID, expiry time.Duration) (string, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}

	url, err := s.storage.PresignedGetURL(ctx, p.FilePath, expiry)
	if err != nil {
		return "", fmt.Errorf("presign prescription document: %w", err)
	}

	return url, nil
}

func detectFileType(file multipart.File) (ext, contentType string, err error) {
	sniff := make([]byte, sniffLen)
	n, readErr := file.Read(sniff)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", "", fmt.Errorf("read uploaded file: %w", readErr)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", "", fmt.Errorf("seek uploaded file: %w", err)
	}

	detected := http.DetectContentType(sniff[:n])
	ext, ok := extensionByContentType[detected]
	if !ok {
		return "", "", ErrUnsupportedFileType
	}

	return ext, detected, nil
}
