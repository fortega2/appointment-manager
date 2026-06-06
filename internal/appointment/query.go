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
			a.slot_id,
			TO_CHAR(s.start_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'YYYY-MM-DD HH24:MI') AS start_time,
			TO_CHAR(s.end_time AT TIME ZONE 'America/Argentina/Buenos_Aires', 'HH24:MI') AS end_time,
			a.patient_id,
			p.first_name || ' ' || p.last_name AS patient_full_name,
			a.professional_id,
			pr.first_name || ' ' || pr.last_name AS professional_full_name,
			a.assistant_id,
			asst.first_name || ' ' || asst.last_name AS assistant_full_name,
			a.status,
			INITCAP(ast.name) AS status_name,
			COALESCE(a.notes, '') AS notes,
			TO_CHAR(a.created_at AT TIME ZONE 'America/Argentina/Buenos_Aires', 'YYYY-MM-DD HH24:MI') AS created_at,
			COALESCE(TO_CHAR(a.updated_at AT TIME ZONE 'America/Argentina/Buenos_Aires', 'YYYY-MM-DD HH24:MI'), '') AS updated_at
		FROM
			public.appointment AS a
		INNER JOIN
			public.slot AS s ON s.id = a.slot_id
		INNER JOIN
			public.patient AS p ON p.id = a.patient_id
		INNER JOIN
			public.professional AS pr ON pr.id = a.professional_id
		INNER JOIN
			public.assistant AS asst ON asst.id = a.assistant_id
		INNER JOIN
			public.appointment_status AS ast ON ast.id = a.status
		ORDER BY
			a.created_at
	`
)

type List struct {
	ID                   string
	SlotID               string
	StartTime            string
	EndTime              string
	PatientID            string
	PatientFullName      string
	ProfessionalID       string
	ProfessionalFullName string
	AssistantID          string
	AssistantFullName    string
	StatusName           string
	Notes                string
	CreatedAt            string
	UpdatedAt            string
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
			&item.SlotID,
			&item.StartTime,
			&item.EndTime,
			&item.PatientID,
			&item.PatientFullName,
			&item.ProfessionalID,
			&item.ProfessionalFullName,
			&item.AssistantID,
			&item.AssistantFullName,
			&item.Status,
			&item.StatusName,
			&item.Notes,
			&item.CreatedAt,
			&item.UpdatedAt,
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
