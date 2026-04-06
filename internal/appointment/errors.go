package appointment

import "errors"

var (
	ErrNilLogger     = errors.New("logger cannot be nil")
	ErrNilService    = errors.New("service cannot be nil")
	ErrNilRepository = errors.New("repository cannot be nil")
	ErrNilPgxPool    = errors.New("pgx pool cannot be nil")

	ErrInvalidPage   = errors.New("invalid page")
	ErrInvalidLimit  = errors.New("invalid limit")
	ErrInvalidStatus = errors.New("invalid status")

	ErrSlotIDRequired         = errors.New("slot id required")
	ErrInvalidSlotID          = errors.New("invalid slot id")
	ErrAppointmentIDRequired  = errors.New("appointment id required")
	ErrInvalidAppointmentID   = errors.New("invalid appointment id")
	ErrPatientIDRequired      = errors.New("patient id required")
	ErrInvalidPatientID       = errors.New("invalid patient id")
	ErrProfessionalIDRequired = errors.New("professional id required")
	ErrInvalidProfessionalID  = errors.New("invalid professional id")
	ErrAssistantIDRequired    = errors.New("assistant id required")
	ErrInvalidAssistantID     = errors.New("invalid assistant id")

	ErrMultipleActiveAppointmentsDetected = errors.New("patient cannot have multiple active appointments in overlapping time slots")
	ErrSlotBlocked                        = errors.New("slot is blocked")
	ErrSlotWithoutAvailability            = errors.New("slot has no available spots")
	ErrInvalidAppointmentReference        = errors.New("invalid appointment reference")
	ErrAppointmentStatusChanged           = errors.New("appointment status changed")
	ErrAppointmentCannotAttendNow         = errors.New("appointment can only be attended during slot time")
	ErrAppointmentCannotAttendWithStatus  = errors.New("appointment cannot be attended from current status")
	ErrAppointmentCannotCancelWithStatus  = errors.New("appointment cannot be cancelled from current status")
)
