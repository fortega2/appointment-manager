package health

import "errors"

var (
	ErrNilLogger         = errors.New("logger cannot be nil")
	ErrNilReadinessCheck = errors.New("readiness check cannot be nil")
	ErrNilPgxPool        = errors.New("pgx pool cannot be nil")
)
