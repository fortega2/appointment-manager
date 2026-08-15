package ratelimit

import "errors"

var (
	ErrInvalidAccountBurst  = errors.New("account burst must be positive")
	ErrInvalidAccountRefill = errors.New("account refill must be positive")
	ErrInvalidIPBurst       = errors.New("ip burst must be positive")
	ErrInvalidIPRefill      = errors.New("ip refill must be positive")
	ErrInvalidMaxEntries    = errors.New("max entries must be positive")
)
