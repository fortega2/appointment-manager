package appointment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	listAppointmentsGridQuery string = `
		SELECT
			a.id,
			s.start_time,
			s.end_time,
			p.first_name || ' ' || p.last_name AS patient_full_name,
			pr.first_name || ' ' || pr.last_name AS professional_full_name,
			a.status
		FROM
			public.appointment AS a
		INNER JOIN
			public.slot AS s ON s.id = a.slot_id
		INNER JOIN
			public.patient AS p ON p.id = a.patient_id
		INNER JOIN
			public.professional AS pr ON pr.id = a.professional_id
		WHERE
			a.status = $1
			AND s.end_time > NOW()
		ORDER BY
			a.created_at DESC
	`

	// The status filter is what makes this answer "who did the clinic call
	// off", not "who is cancelled on this slot". A patient who cancelled their
	// own appointment earlier sits at CANCELLED and is deliberately excluded --
	// telling them the clinic cancelled would be wrong.
	//
	// The professional comes from the slot rather than the appointment: it is
	// the owner of the cancelled slot who the message is about.
	slotCancellationRecipientsQuery string = `
		SELECT
			s.start_time,
			s.end_time,
			pr.first_name || ' ' || pr.last_name AS professional_full_name,
			p.id,
			p.first_name || ' ' || p.last_name AS patient_full_name,
			p.email,
			p.phone
		FROM
			public.appointment AS a
		INNER JOIN
			public.slot AS s ON s.id = a.slot_id
		INNER JOIN
			public.patient AS p ON p.id = a.patient_id
		INNER JOIN
			public.professional AS pr ON pr.id = s.professional_id
		WHERE
			a.slot_id = $1
			AND a.status = $2
		ORDER BY
			p.last_name,
			p.first_name
	`
)

type List struct {
	ID                   string
	StartTime            time.Time
	EndTime              time.Time
	PatientFullName      string
	ProfessionalFullName string
	Status               Status
}

// SlotCancellationRecipient is one patient the clinic owes a message after
// cancelling a slot. Email is a pointer because patient.email is nullable in
// the schema, unlike phone -- a patient genuinely may have no address on file.
type SlotCancellationRecipient struct {
	FullName  string
	Phone     string
	PatientID string
	Email     *string
}

// SlotCancellation groups the affected patients with the details of the slot
// they lost. The slot and professional are held once rather than repeated on
// every recipient, since one cancellation is what the whole group shares.
type SlotCancellation struct {
	StartTime            time.Time
	EndTime              time.Time
	Recipients           []SlotCancellationRecipient
	ProfessionalFullName string
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
	rows, err := q.pool.Query(ctx, listAppointmentsGridQuery, StatusConfirmed)
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

// SlotCancellationRecipients returns the patients whose appointments the clinic
// cancelled along with the given slot, together with the details of that slot.
//
// It reads only StatusCancelledByClinic, so patients who had cancelled their own
// appointment on the same slot are excluded: they are already CANCELLED and the
// clinic owes them nothing.
//
// A slot nobody booked yields an empty result and a nil error, matching how the
// bulk cancel treats zero rows.
func (q *Query) SlotCancellationRecipients(ctx context.Context, slotID uuid.UUID) (SlotCancellation, error) {
	rows, err := q.pool.Query(ctx, slotCancellationRecipientsQuery, slotID, StatusCancelledByClinic)
	if err != nil {
		return SlotCancellation{}, fmt.Errorf("slot cancellation recipients: query: %w", err)
	}
	defer rows.Close()

	cancellation := SlotCancellation{Recipients: make([]SlotCancellationRecipient, 0)}
	for rows.Next() {
		var recipient SlotCancellationRecipient
		// The slot and professional columns repeat on every row of the join, so
		// they are rescanned and overwritten rather than read separately.
		if err := rows.Scan(
			&cancellation.StartTime,
			&cancellation.EndTime,
			&cancellation.ProfessionalFullName,
			&recipient.PatientID,
			&recipient.FullName,
			&recipient.Email,
			&recipient.Phone,
		); err != nil {
			return SlotCancellation{}, fmt.Errorf("slot cancellation recipients: scan: %w", err)
		}
		cancellation.Recipients = append(cancellation.Recipients, recipient)
	}

	if err := rows.Err(); err != nil {
		return SlotCancellation{}, fmt.Errorf("slot cancellation recipients: iterate: %w", err)
	}

	return cancellation, nil
}
