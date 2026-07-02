package prescription

import (
	"strings"

	"appointment-manager/internal/domain"

	"github.com/google/uuid"
)

type Status int16

const (
	StatusActive Status = iota + 1
	StatusCompleted
	StatusCancelled
)

type Prescription struct {
	ID            uuid.UUID
	PatientID     uuid.UUID
	FilePath      string
	TotalSessions int
	Status        Status
}

func New(patientID uuid.UUID, filePath string, totalSessions int) (*Prescription, error) {
	if patientID == uuid.Nil {
		return nil, ErrNilPatientID
	}

	trimmedFilePath := strings.TrimSpace(filePath)
	if trimmedFilePath == "" {
		return nil, ErrEmptyFilePath
	}

	if totalSessions <= 0 {
		return nil, ErrInvalidTotalSessions
	}

	return &Prescription{
		ID:            domain.NewID(),
		PatientID:     patientID,
		FilePath:      trimmedFilePath,
		TotalSessions: totalSessions,
		Status:        StatusActive,
	}, nil
}
