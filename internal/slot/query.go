package slot

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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

func (q *Query) List(ctx context.Context) ([]ListDTO, error) {
	const query string = `
		SELECT
			s.id,
			s.professional_id,
			p.first_name || ' ' || p.last_name AS professional_name,
			TO_CHAR(s.date, 'YYYY-MM-DD') AS date,
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
	rows, err := q.pool.Query(ctx, query)
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

func (q *Query) GetByID(ctx context.Context, id uuid.UUID) (ListDTO, error) {
	const query string = `
		SELECT
			s.id,
			s.professional_id,
			p.first_name || ' ' || p.last_name AS professional_name,
			TO_CHAR(s.date, 'YYYY-MM-DD') AS date,
    		TO_CHAR(s.start_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS start_time,
    		TO_CHAR(s.end_time   AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS end_time,
			s.max_capacity,
			s.blocked
		FROM
			slot s
		JOIN
			professional p ON p.id = s.professional_id
		WHERE
			s.id = $1
	`
	var dto ListDTO
	if err := q.pool.QueryRow(ctx, query, id).Scan(
		&dto.ID,
		&dto.ProfessionalID,
		&dto.ProfessionalName,
		&dto.Date,
		&dto.StartTime,
		&dto.EndTime,
		&dto.MaxCapacity,
		&dto.Blocked,
	); err != nil {
		return ListDTO{}, fmt.Errorf("get slot by id: %w", err)
	}

	return dto, nil
}

func (q *Query) ListAvailable(ctx context.Context) ([]ListDTO, error) {
	const query string = `
		SELECT
			s.id,
			s.professional_id,
			p.first_name || ' ' || p.last_name AS professional_name,
			TO_CHAR(s.date, 'YYYY-MM-DD') AS date,
    		TO_CHAR(s.start_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS start_time,
    		TO_CHAR(s.end_time   AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS end_time,
			s.max_capacity,
			s.blocked
		FROM
			slot s
		JOIN
			professional p ON p.id = s.professional_id
		WHERE
			s.blocked = false
			AND s.end_time > NOW()
		ORDER BY
			s.date, s.start_time
	`
	rows, err := q.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list available slots: %w", err)
	}
	defer rows.Close()

	list := make([]ListDTO, 0)
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
			return nil, fmt.Errorf("scan available slot: %w", err)
		}
		list = append(list, dto)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate available slots: %w", err)
	}

	return list, nil
}
