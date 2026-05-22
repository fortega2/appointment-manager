package healthinsurance

import "errors"

var (
	ErrNilPgxPool = errors.New("pgx pool cannot be nil")
)
