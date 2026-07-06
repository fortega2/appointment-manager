package prescription

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	listActiveBalancesQuery = `
		SELECT
			b.patient_id,
			p.first_name || ' ' || p.last_name AS patient_full_name,
			b.prescription_id,
			b.total_sessions,
			b.remaining_sessions
		FROM
			public.patient_session_balance b
		INNER JOIN
			public.patient p ON p.id = b.patient_id
		ORDER BY
			p.first_name, p.last_name
	`

	listEligiblePatientsQuery = `
		SELECT
			p.id,
			p.first_name || ' ' || p.last_name AS full_name,
			b.remaining_sessions
		FROM
			public.patient_session_balance b
		INNER JOIN
			public.patient p ON p.id = b.patient_id
		WHERE
			b.remaining_sessions > 0
		ORDER BY
			p.first_name, p.last_name
	`

	listAvailablePatientsQuery = `
		SELECT
			p.id,
			p.first_name || ' ' || p.last_name AS full_name
		FROM
			public.patient p
		WHERE
			NOT EXISTS (
				SELECT 1 FROM public.prescription pr
				WHERE pr.patient_id = p.id AND pr.status = 1
			)
		ORDER BY
			p.first_name, p.last_name
	`
)

type Balance struct {
	PatientID         string
	PatientFullName   string
	PrescriptionID    string
	TotalSessions     int
	RemainingSessions int
}

type PatientOption struct {
	ID    string
	Label string
}

// EligiblePatient is a patient with a remaining session balance, used to
// populate the appointment booking form's patient dropdown and to show the
// selected patient's remaining sessions without a round trip.
type EligiblePatient struct {
	ID                string
	Label             string
	RemainingSessions int
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

func (q *Query) ListActiveBalances(ctx context.Context) ([]Balance, error) {
	rows, err := q.pool.Query(ctx, listActiveBalancesQuery)
	if err != nil {
		return nil, fmt.Errorf("list active balances: query: %w", err)
	}
	defer rows.Close()

	balances := make([]Balance, 0)
	for rows.Next() {
		var b Balance
		if err := rows.Scan(
			&b.PatientID,
			&b.PatientFullName,
			&b.PrescriptionID,
			&b.TotalSessions,
			&b.RemainingSessions,
		); err != nil {
			return nil, fmt.Errorf("list active balances: scan: %w", err)
		}
		balances = append(balances, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active balances: iterate: %w", err)
	}

	return balances, nil
}

func (q *Query) EligiblePatients(ctx context.Context) ([]EligiblePatient, error) {
	rows, err := q.pool.Query(ctx, listEligiblePatientsQuery)
	if err != nil {
		return nil, fmt.Errorf("list eligible patients: query: %w", err)
	}
	defer rows.Close()

	patients := make([]EligiblePatient, 0)
	for rows.Next() {
		var p EligiblePatient
		if err := rows.Scan(&p.ID, &p.Label, &p.RemainingSessions); err != nil {
			return nil, fmt.Errorf("list eligible patients: scan: %w", err)
		}
		patients = append(patients, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list eligible patients: iterate: %w", err)
	}

	return patients, nil
}

func (q *Query) AvailablePatients(ctx context.Context) ([]PatientOption, error) {
	return q.listPatientOptions(ctx, listAvailablePatientsQuery, "available patients")
}

func (q *Query) listPatientOptions(ctx context.Context, query, operation string) ([]PatientOption, error) {
	rows, err := q.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list %s: query: %w", operation, err)
	}
	defer rows.Close()

	options := make([]PatientOption, 0)
	for rows.Next() {
		var o PatientOption
		if err := rows.Scan(&o.ID, &o.Label); err != nil {
			return nil, fmt.Errorf("list %s: scan: %w", operation, err)
		}
		options = append(options, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %s: iterate: %w", operation, err)
	}

	return options, nil
}
