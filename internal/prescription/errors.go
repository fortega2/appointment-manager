package prescription

import "errors"

var (
	ErrNilPatientID         = errors.New("patient ID cannot be nil")
	ErrEmptyFilePath        = errors.New("file path cannot be empty")
	ErrInvalidTotalSessions = errors.New("total sessions must be greater than zero")

	ErrNilPgxPool               = errors.New("pgx pool cannot be nil")
	ErrNilPrescription          = errors.New("prescription cannot be nil")
	ErrPrescriptionNotFound     = errors.New("prescription not found")
	ErrActivePrescriptionExists = errors.New("patient already has an active prescription")
	ErrNoActivePrescription     = errors.New("patient has no active prescription")
	ErrInvalidPatient           = errors.New("patient does not exist")

	ErrNilRepository       = errors.New("repository cannot be nil")
	ErrNilStorageClient    = errors.New("storage client cannot be nil")
	ErrNilFile             = errors.New("uploaded file cannot be nil")
	ErrNilFileHeader       = errors.New("uploaded file header cannot be nil")
	ErrUnsupportedFileType = errors.New("unsupported file type")

	ErrNilLogger  = errors.New("logger cannot be nil")
	ErrNilService = errors.New("service cannot be nil")
	ErrNilQuery   = errors.New("query cannot be nil")
)
