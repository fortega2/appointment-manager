package slot

import "errors"

var (
	ErrInvalidProfessionalID = errors.New("professional ID cannot be nil")
	ErrInvalidTimeRange      = errors.New("end time must be after start time")
	ErrInvalidMaxCapacity    = errors.New("max capacity must be greater than zero")
	ErrInvalidDate           = errors.New("date cannot be zero")
	ErrDateTimeInconsistency = errors.New("date must match the date part of start time")
	ErrNilSlot               = errors.New("slot cannot be nil")
	ErrSlotOverlaps          = errors.New("the slot overlaps with an existing one")
	ErrSlotNotFound          = errors.New("slot not found")
	ErrSlotAlreadyCancelled  = errors.New("slot is already cancelled")

	ErrNilPgxPool = errors.New("pgx pool cannot be nil")

	ErrNilLogger             = errors.New("logger cannot be nil")
	ErrNilRepository         = errors.New("repository cannot be nil")
	ErrNilQuery              = errors.New("query cannot be nil")
	ErrNilProfessionalRepo   = errors.New("professional repository cannot be nil")
	ErrNilCancelAppointments = errors.New("cancel appointments function cannot be nil")
	ErrNilSendNotification   = errors.New("send notification function cannot be nil")
)
