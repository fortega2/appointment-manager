package slot

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Query struct {
	pool *pgxpool.Pool
}

func NewQuery(pool *pgxpool.Pool) (*Query, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &Query{pool: pool}, nil
}

type ListDTO struct {
	ID               string
	ProfessionalID   string
	ProfessionalName string
	Date             string
	StartTime        string
	EndTime          string
	MaxCapacity      int16
	Blocked          bool
}

func (r *Query) List(ctx context.Context) ([]ListDTO, error) {
	const query string = `
		SELECT
			s.id,
			s.professional_id,
			p.first_name || ' ' || p.last_name AS professional_name,
			TO_CHAR(s.date, 'DD/MM/YYYY') AS date,
    		TO_CHAR(s.start_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS start_time,
    		TO_CHAR(s.end_time   AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS end_time,
			s.max_capacity,
			s.blocked
		FROM
			slot s
		JOIN
			professional p ON p.id = s.professional_id
		ORDER BY
			s.date, s.start_time
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list slots: %w", err)
	}
	defer rows.Close()

	var list = make([]ListDTO, 0)
	for rows.Next() {
		var dto ListDTO
		if err := rows.Scan(
			&dto.ID,
			&dto.ProfessionalID,
			&dto.ProfessionalName,
			&dto.Date,
			&dto.StartTime,
			&dto.EndTime,
			&dto.MaxCapacity,
			&dto.Blocked,
		); err != nil {
			return nil, fmt.Errorf("scan slot: %w", err)
		}
		list = append(list, dto)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slots: %w", err)
	}

	return list, nil
}
