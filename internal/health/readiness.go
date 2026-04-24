package health

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPgxReadinessCheck(pool *pgxpool.Pool) (CheckReady, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return pool.Ping, nil
}
