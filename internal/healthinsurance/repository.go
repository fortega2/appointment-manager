package healthinsurance

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &Repository{pool: pool}, nil
}

func (r *Repository) List(ctx context.Context) ([]HealthInsurance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			h.id,
			h.name
		FROM
			health_insurance h
		ORDER BY
			h.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list: query: %w", err)
	}
	defer rows.Close()

	var healthInsurances []HealthInsurance
	for rows.Next() {
		var h HealthInsurance
		if err := rows.Scan(&h.ID, &h.Name); err != nil {
			return nil, fmt.Errorf("list: scan: %w", err)
		}
		healthInsurances = append(healthInsurances, h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list: rows error: %w", err)
	}

	return healthInsurances, nil
}
