package appointment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	listAppointmentsGridQuery string = `
		SELECT
			a.id,
			TO_CHAR(s.start_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'YYYY-MM-DD HH24:MI') AS start_time,
			TO_CHAR(s.end_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS end_time,
			p.first_name || ' ' || p.last_name AS patient_full_name,
			pr.first_name || ' ' || pr.last_name AS professional_full_name,
			a.status,
			INITCAP(ast.name) AS status_name
		FROM
			public.appointment AS a
		INNER JOIN
			public.slot AS s ON s.id = a.slot_id
		INNER JOIN
			public.patient AS p ON p.id = a.patient_id
		INNER JOIN
			public.professional AS pr ON pr.id = a.professional_id
		INNER JOIN
			public.appointment_status AS ast ON ast.id = a.status
		ORDER BY
			a.created_at DESC
	`
)

type List struct {
	ID                   string
	StartTime            string
	EndTime              string
	PatientFullName      string
	ProfessionalFullName string
	StatusName           string
	Status               int
}

type Query struct {
	pool *pgxpool.Pool
}

func NewQuery(pool *pgxpool.Pool) (*Query, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &Query{pool: pool}, nil
}

func (q *Query) List(ctx context.Context) ([]List, error) {
	rows, err := q.pool.Query(ctx, listAppointmentsGridQuery)
	if err != nil {
		return nil, fmt.Errorf("list: query appointments: %w", err)
	}
	defer rows.Close()

	appointments := make([]List, 0)
	for rows.Next() {
		var item List
		if err := rows.Scan(
			&item.ID,
			&item.StartTime,
			&item.EndTime,
			&item.PatientFullName,
			&item.ProfessionalFullName,
			&item.Status,
			&item.StatusName,
		); err != nil {
			return nil, fmt.Errorf("list: scan appointment: %w", err)
		}
		appointments = append(appointments, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list: iterate appointments: %w", err)
	}

	return appointments, nil
}
