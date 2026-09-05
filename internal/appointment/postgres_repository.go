package appointment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"appointment-manager/internal/outbox"
	"appointment-manager/internal/slot"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	pgErrUniqueViolation     = "23505"
	pgErrForeignKeyViolation = "23503"

	constraintAppointmentSlotPatientActive = "idx_appointment_slot_patient_active"
	constraintAppointmentSlotFK            = "fk_appointment_slot"
	constraintAppointmentPatientFK         = "fk_appointment_patient"
	constraintAppointmentProfessionalFK    = "fk_appointment_professional"
	constraintAppointmentAssistantFK       = "fk_appointment_assistant"
	constraintPrescriptionActivePerPatient = "idx_prescription_active_per_patient"

	// Mirror prescription.StatusActive and prescription.StatusCompleted; kept as
	// local literals so the appointment repository stays decoupled from the
	// prescription package, consistent with how it queries the patient and slot
	// tables directly.
	prescriptionStatusActive    int16 = 1
	prescriptionStatusCompleted int16 = 2

	listAppointmentsQuery = `
		SELECT
			id,
			slot_id,
			patient_id,
			professional_id,
			assistant_id,
			status,
			notes
		FROM
			appointment
		WHERE
			status = $1
		ORDER BY
			created_at
		LIMIT
			$2
		OFFSET
			$3
	`
	insertAppointmentQuery = `
		INSERT INTO appointment (
			id,
			slot_id,
			patient_id,
			professional_id,
			assistant_id,
			notes,
			prescription_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	selectActivePrescriptionForUpdateQuery = `
		SELECT
			id,
			total_sessions
		FROM
			prescription
		WHERE
			patient_id = $1
			AND status = 1
		FOR UPDATE
	`
	countConsumedSessionsQuery = `
		SELECT
			COUNT(*)
		FROM
			appointment
		WHERE
			prescription_id = $1
			AND status IN ($2, $3, $4)
	`
	completePrescriptionQuery = `
		UPDATE
			prescription
		SET
			status = $1
		WHERE
			id = $2
	`
	// COMPLETED is a cached answer to "are all authorized sessions used up?",
	// written when a booking consumes the last one. Cancelling that booking
	// makes the cached answer wrong, so it has to be recomputed from the
	// appointments themselves -- this statement is the other edge of
	// completePrescriptionQuery.
	//
	// Each guard earns its place:
	//   - status = $3 (COMPLETED) keeps this off ACTIVE and CANCELLED
	//     prescriptions, so it is inert on the overwhelmingly common case and a
	//     no-op when two cancellations race to reopen the same prescription.
	//   - The NOT EXISTS clause is what forfeits the freed session when the
	//     patient has already been issued a newer ACTIVE prescription. Reopening
	//     would violate idx_prescription_active_per_patient (one ACTIVE per
	//     patient) and cost the cancellation, which matters more than the
	//     session; the patient has moved on to the newer prescription anyway.
	//   - The COUNT is the authority on consumption. $4/$5/$6 bind the same
	//     three statuses as countConsumedSessionsQuery, and the
	//     patient_session_balance view counts the identical set. Because it
	//     reads real rows, ABSENT and ATTENDED exclude themselves: they still
	//     consume, so the count is unchanged and no row matches.
	reopenPrescriptionQuery = `
		UPDATE
			prescription
		SET
			status = $1
		WHERE
			id = $2
			AND status = $3
			AND NOT EXISTS (
				SELECT
					1
				FROM
					prescription AS other_prescription
				WHERE
					other_prescription.patient_id = prescription.patient_id
					AND other_prescription.status = $1
					AND other_prescription.id <> prescription.id
			)
			AND (
				SELECT
					COUNT(*)
				FROM
					appointment
				WHERE
					appointment.prescription_id = prescription.id
					AND appointment.status IN ($4, $5, $6)
			) < prescription.total_sessions
	`
	selectPatientForUpdateQuery = `
		SELECT
			id
		FROM
			patient
		WHERE
			id = $1
		FOR UPDATE
	`
	selectSlotForUpdateQuery = `
		SELECT
			blocked,
			max_capacity
		FROM
			slot
		WHERE
			id = $1
		FOR UPDATE
	`
	countConfirmedInSlotQuery = `
		SELECT
			COUNT(*)
		FROM
			appointment
		WHERE
			slot_id = $1
			AND status = $2
	`
	hasOverlappingConfirmedQuery = `
		SELECT
			EXISTS (
				SELECT
					1
				FROM
					appointment AS occupied_appointment
				JOIN slot AS occupied_slot ON occupied_slot.id = occupied_appointment.slot_id
				JOIN slot AS target_slot ON target_slot.id = $2
				WHERE
					occupied_appointment.patient_id = $1
					AND occupied_appointment.status = $3
					AND occupied_slot.start_time < target_slot.end_time
					AND occupied_slot.end_time > target_slot.start_time
			)
	`
	selectAppointmentWindowQuery = `
		SELECT
			slot.start_time,
			slot.end_time,
			appointment.status
		FROM
			appointment
		JOIN
			slot ON slot.id = appointment.slot_id
		WHERE
			appointment.id = $1
	`
	updateAppointmentStatusQuery = `
		UPDATE
			appointment
		SET
			status = $1,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			id = $2
			AND status = $3
		RETURNING
			prescription_id
	`
	selectAppointmentStatusQuery = `
		SELECT
			status
		FROM
			appointment
		WHERE
			id = $1
	`
	// Blocked slots are excluded so this sweep and CancelOnBlockedSlots cannot
	// both claim the same row. They run on the same interval, so without this
	// the winner would be arbitrary -- and the outcomes are not equivalent:
	// ABSENT blames the patient and consumes their session, while the clinic is
	// the one who withdrew the slot. Anything stranded on a blocked slot belongs
	// to the reconciliation sweep, whatever its end time.
	expireOverdueAppointmentsQuery = `
		UPDATE
			appointment AS a
		SET
			status = $1,
			updated_at = CURRENT_TIMESTAMP
		FROM
			slot AS s
		WHERE
			s.id = a.slot_id
			AND a.status = $2
			AND s.end_time < CURRENT_TIMESTAMP
			AND s.blocked = FALSE
	`
	// The CONFIRMED predicate is written as the literal 1 rather than a
	// placeholder so it provably implies idx_appointment_slot_confirmed's own
	// WHERE status = 1. Postgres can only match a partial index when it can
	// prove that implication, which a parameter hides from a generic plan --
	// with $n these statements fall back to a sequential scan once the planner
	// stops re-planning them. The value is pinned by the appointment_status
	// lookup table and mirrored by StatusConfirmed.
	cancelAppointmentsBySlotQuery = `
		UPDATE
			appointment
		SET
			status = $1,
			updated_at = CURRENT_TIMESTAMP
		WHERE
			slot_id = $2
			AND status = 1
		RETURNING
			prescription_id,
			slot_id
	`
	cancelAppointmentsOnBlockedSlotsQuery = `
		UPDATE
			appointment AS a
		SET
			status = $1,
			updated_at = CURRENT_TIMESTAMP
		FROM
			slot AS s
		WHERE
			s.id = a.slot_id
			AND a.status = 1
			AND s.blocked = TRUE
		RETURNING
			a.prescription_id,
			a.slot_id
	`
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrNilPgxPool
	}

	return &PostgresRepository{pool: pool}, nil
}

func (r *PostgresRepository) List(ctx context.Context, filter ListFilter) ([]Appointment, error) {
	offset := (filter.Page - 1) * filter.Limit

	rows, err := r.pool.Query(ctx, listAppointmentsQuery, filter.Status, filter.Limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query appointments: %w", err)
	}
	defer rows.Close()

	appointments := make([]Appointment, 0, filter.Limit)
	for rows.Next() {
		var item Appointment
		if err := rows.Scan(
			&item.ID,
			&item.SlotID,
			&item.PatientID,
			&item.ProfessionalID,
			&item.AssistantID,
			&item.Status,
			&item.Notes,
		); err != nil {
			return nil, fmt.Errorf("scan appointment: %w", err)
		}
		appointments = append(appointments, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate appointments: %w", err)
	}

	return appointments, nil
}

func (r *PostgresRepository) Create(ctx context.Context, appoint Appointment) (uuid.UUID, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil(), fmt.Errorf("begin create appointment transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockPatientForUpdate(ctx, tx, appoint.PatientID); err != nil {
		return uuid.Nil(), err
	}

	prescriptionID, isLastSession, err := reserveActivePrescriptionSession(ctx, tx, appoint.PatientID)
	if err != nil {
		return uuid.Nil(), err
	}

	if err := validateSlotForBooking(ctx, tx, appoint.PatientID, appoint.SlotID); err != nil {
		return uuid.Nil(), err
	}

	if _, err := tx.Exec(
		ctx,
		insertAppointmentQuery,
		appoint.ID,
		appoint.SlotID,
		appoint.PatientID,
		appoint.ProfessionalID,
		appoint.AssistantID,
		normalizeNotes(appoint.Notes),
		prescriptionID,
	); err != nil {
		if mappedErr := mapCreateAppointmentConstraintError(err); mappedErr != nil {
			return uuid.Nil(), mappedErr
		}
		return uuid.Nil(), fmt.Errorf("create db appointment: %w", err)
	}

	// This booking consumed the last authorized session, so the prescription
	// is completed within the same transaction. This frees the partial unique
	// index so the patient can later be assigned a new active prescription.
	if isLastSession {
		if err := completePrescription(ctx, tx, prescriptionID); err != nil {
			return uuid.Nil(), err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil(), fmt.Errorf("commit create appointment transaction: %w", err)
	}

	return appoint.ID, nil
}

func (r *PostgresRepository) GetWindow(ctx context.Context, appointmentID uuid.UUID) (Window, error) {
	var startTime, endTime time.Time
	var status Status
	if err := r.pool.QueryRow(
		ctx,
		selectAppointmentWindowQuery,
		appointmentID,
	).Scan(&startTime, &endTime, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Window{}, ErrInvalidAppointmentReference
		}
		return Window{}, fmt.Errorf("fetch appointment slot time for status validation: %w", err)
	}

	return Window{
		StartTime: startTime,
		EndTime:   endTime,
		Status:    status,
	}, nil
}

// UpdateStatus shares a transaction with the reopen because the reopen decision
// is read from the appointment rows: it has to see this cancellation already
// applied, and it must not survive on its own if the status change is rolled
// back.
func (r *PostgresRepository) UpdateStatus(ctx context.Context, appointmentID uuid.UUID, newStatus, expectedStatus Status) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin update appointment status transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var prescriptionID uuid.UUID
	if err := tx.QueryRow(
		ctx,
		updateAppointmentStatusQuery,
		newStatus,
		appointmentID,
		expectedStatus,
	).Scan(&prescriptionID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("update appointment status: %w", err)
		}

		// Read inside this transaction rather than from the pool: taking a
		// connection from the pool here would self-deadlock a pool sized at one.
		if _, readErr := readStatus(ctx, tx, appointmentID); readErr != nil {
			return readErr
		}

		return ErrAppointmentStatusChanged
	}

	// Only a cancellation hands the session back. ABSENT and ATTENDED still
	// consume one, so the reopen would match nothing for them -- skipping it
	// keeps a correlated count off every attend and every late cancellation.
	if newStatus.IsCancelled() {
		if err := reopenFreedPrescriptions(ctx, tx, []uuid.UUID{prescriptionID}); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update appointment status transaction: %w", err)
	}

	return nil
}

// ExpireOverdue marks every CONFIRMED appointment whose slot has already ended
// as ABSENT (a no-show) in a single atomic statement and returns the number of
// rows updated. The status = CONFIRMED predicate makes the update
// concurrency-safe: a row concurrently attended or cancelled no longer matches
// and is skipped. Zero rows is a valid, non-error result.
func (r *PostgresRepository) ExpireOverdue(ctx context.Context) (int64, error) {
	cmd, err := r.pool.Exec(ctx, expireOverdueAppointmentsQuery, StatusAbsent, StatusConfirmed)
	if err != nil {
		return 0, fmt.Errorf("expire overdue appointments: %w", err)
	}

	return cmd.RowsAffected(), nil
}

// CancelBySlot marks every CONFIRMED appointment booked on the given slot as
// CANCELLED BY CLINIC in a single atomic statement and returns the number of
// rows updated. It is the counterpart of cancelling the slot itself: the clinic
// withdrew the slot, so patients are never marked ABSENT here, regardless of
// how close the slot is — the 24h rule in Service.Cancel deliberately does not
// apply. The distinct status is what keeps these rows separable afterwards from
// appointments the patients themselves had already cancelled on the same slot.
// The status = CONFIRMED predicate makes the update concurrency-safe: a row
// concurrently attended or cancelled no longer matches and is skipped. Zero
// rows is a valid, non-error result (a slot with no bookings).
func (r *PostgresRepository) CancelBySlot(ctx context.Context, slotID uuid.UUID) (int64, error) {
	return r.cancelAndReopen(ctx, "cancel appointments by slot", cancelAppointmentsBySlotQuery, StatusCancelledByClinic, slotID)
}

// CancelOnBlockedSlots reconciles appointments left CONFIRMED on a slot that is
// already blocked, and returns the number of rows updated. Cancelling a slot is
// two independent steps (block the slot, then cancel its appointments), so a
// failure between them leaves the pair inconsistent; this sweep converges that
// state on the next worker tick. It is idempotent — once no CONFIRMED
// appointment sits on a blocked slot, it updates nothing and returns zero. It
// writes the same CANCELLED BY CLINIC status as CancelBySlot, since the rows it
// converges are ones that cancel should have caught.
func (r *PostgresRepository) CancelOnBlockedSlots(ctx context.Context) (int64, error) {
	return r.cancelAndReopen(ctx, "cancel appointments on blocked slots", cancelAppointmentsOnBlockedSlotsQuery, StatusCancelledByClinic)
}

// cancelAndReopen has no IsCancelled gate like UpdateStatus: both callers always
// write CANCELLED BY CLINIC, which always frees the session.
func (r *PostgresRepository) cancelAndReopen(ctx context.Context, operation, query string, args ...any) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin %s transaction: %w", operation, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	cancelled, err := collectCancelledAppointments(ctx, tx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", operation, err)
	}

	if err := reopenFreedPrescriptions(ctx, tx, cancelled.prescriptionIDs); err != nil {
		return 0, fmt.Errorf("%s: %w", operation, err)
	}

	if err := emitAppointmentsCancelledEvents(ctx, tx, cancelled.slotIDs); err != nil {
		return 0, fmt.Errorf("%s: %w", operation, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit %s transaction: %w", operation, err)
	}

	return int64(len(cancelled.prescriptionIDs)), nil
}

// cancelledAppointments is what one cancellation touched. Both slices are raw
// RETURNING output, so they may repeat.
type cancelledAppointments struct {
	prescriptionIDs []uuid.UUID
	slotIDs         []uuid.UUID
}

// collectCancelledAppointments drains the RETURNING rows before returning: an
// open result set makes the transaction reject the reopen that follows as busy.
func collectCancelledAppointments(ctx context.Context, tx pgx.Tx, query string, args ...any) (cancelledAppointments, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return cancelledAppointments{}, err
	}
	defer rows.Close()

	var cancelled cancelledAppointments
	for rows.Next() {
		var prescriptionID, slotID uuid.UUID
		if err := rows.Scan(&prescriptionID, &slotID); err != nil {
			return cancelledAppointments{}, err
		}

		cancelled.prescriptionIDs = append(cancelled.prescriptionIDs, prescriptionID)
		cancelled.slotIDs = append(cancelled.slotIDs, slotID)
	}
	if err := rows.Err(); err != nil {
		return cancelledAppointments{}, err
	}

	return cancelled, nil
}

// emitAppointmentsCancelledEvents records one event per affected slot; both
// callers repeat a slot per booking, and the sweep converges several at once.
func emitAppointmentsCancelledEvents(ctx context.Context, tx pgx.Tx, slotIDs []uuid.UUID) error {
	for _, slotID := range sortedUniqueIDs(slotIDs) {
		event := outbox.Event{
			AggregateType: slot.OutboxAggregate,
			EventType:     slot.EventAppointmentsCancelled,
			AggregateID:   slotID,
		}

		if err := outbox.Insert(ctx, tx, event); err != nil {
			return fmt.Errorf("emit slot cancelled event: %w", err)
		}
	}

	return nil
}

func readStatus(ctx context.Context, tx pgx.Tx, appointmentID uuid.UUID) (Status, error) {
	var status Status
	if err := tx.QueryRow(ctx, selectAppointmentStatusQuery, appointmentID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrInvalidAppointmentReference
		}

		return 0, fmt.Errorf("read appointment status: %w", err)
	}

	return status, nil
}

func lockPatientForUpdate(ctx context.Context, tx pgx.Tx, patientID uuid.UUID) error {
	var selectedPatientID uuid.UUID
	if err := tx.QueryRow(ctx, selectPatientForUpdateQuery, patientID).Scan(&selectedPatientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidAppointmentReference
		}
		return fmt.Errorf("lock patient for update: %w", err)
	}

	return nil
}

// validateSlotForBooking enforces the slot-level booking rules against the
// locked slot: it must not be blocked, must have remaining capacity, and must
// not overlap another confirmed appointment for the same patient.
func validateSlotForBooking(ctx context.Context, tx pgx.Tx, patientID, slotID uuid.UUID) error {
	blocked, maxCapacity, err := fetchSlotRulesForUpdate(ctx, tx, slotID)
	if err != nil {
		return err
	}
	if blocked {
		return ErrSlotBlocked
	}

	confirmedAppointments, err := countConfirmedAppointmentsInSlot(ctx, tx, slotID)
	if err != nil {
		return err
	}
	if confirmedAppointments >= int64(maxCapacity) {
		return ErrSlotWithoutAvailability
	}

	hasOverlappingAppointment, err := hasOverlappingConfirmedAppointment(ctx, tx, patientID, slotID)
	if err != nil {
		return err
	}
	if hasOverlappingAppointment {
		return ErrMultipleActiveAppointmentsDetected
	}

	return nil
}

// reserveActivePrescriptionSession locks the patient's active prescription,
// verifies it still has an authorized session available, and reports whether
// this booking consumes its last one (so the caller can complete it).
func reserveActivePrescriptionSession(ctx context.Context, tx pgx.Tx, patientID uuid.UUID) (uuid.UUID, bool, error) {
	prescriptionID, totalSessions, err := lockActivePrescriptionForUpdate(ctx, tx, patientID)
	if err != nil {
		return uuid.Nil(), false, err
	}

	consumedSessions, err := countConsumedSessions(ctx, tx, prescriptionID)
	if err != nil {
		return uuid.Nil(), false, err
	}
	if consumedSessions >= int64(totalSessions) {
		return uuid.Nil(), false, ErrNoRemainingSessions
	}

	isLastSession := consumedSessions+1 >= int64(totalSessions)

	return prescriptionID, isLastSession, nil
}

func lockActivePrescriptionForUpdate(ctx context.Context, tx pgx.Tx, patientID uuid.UUID) (uuid.UUID, int16, error) {
	var prescriptionID uuid.UUID
	var totalSessions int16
	if err := tx.QueryRow(ctx, selectActivePrescriptionForUpdateQuery, patientID).Scan(&prescriptionID, &totalSessions); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil(), 0, ErrNoActivePrescription
		}
		return uuid.Nil(), 0, fmt.Errorf("lock active prescription for update: %w", err)
	}

	return prescriptionID, totalSessions, nil
}

func countConsumedSessions(ctx context.Context, tx pgx.Tx, prescriptionID uuid.UUID) (int64, error) {
	var consumedCount int64
	if err := tx.QueryRow(
		ctx,
		countConsumedSessionsQuery,
		prescriptionID,
		StatusConfirmed,
		StatusAbsent,
		StatusAttended,
	).Scan(&consumedCount); err != nil {
		return 0, fmt.Errorf("count consumed sessions: %w", err)
	}

	return consumedCount, nil
}

func completePrescription(ctx context.Context, tx pgx.Tx, prescriptionID uuid.UUID) error {
	if _, err := tx.Exec(ctx, completePrescriptionQuery, prescriptionStatusCompleted, prescriptionID); err != nil {
		return fmt.Errorf("complete prescription: %w", err)
	}

	return nil
}

// reopenFreedPrescriptions gives back prescriptions whose completion stopped
// being true once a cancellation returned a session.
//
// The IDs are deduplicated and sorted so that every caller takes prescription
// row locks in one global order: two bulk cancellations whose freed sets overlap
// would otherwise be free to acquire them in opposite orders and deadlock.
//
// Issuing one statement per prescription rather than a single set-based update
// is also what keeps a patient with two candidate prescriptions from violating
// idx_prescription_active_per_patient. Statements see their predecessors' work,
// so once the first is ACTIVE the second one's NOT EXISTS finds it and declines
// -- the same rule the newer-prescription case follows. A single statement would
// evaluate both against one snapshot, set both ACTIVE, and lose the whole
// cancellation to a unique violation.
func reopenFreedPrescriptions(ctx context.Context, tx pgx.Tx, prescriptionIDs []uuid.UUID) error {
	if len(prescriptionIDs) == 0 {
		return nil
	}

	// The reopen runs inside a savepoint so that losing it cannot cost the
	// cancellation. The NOT EXISTS guard is evaluated against this transaction's
	// snapshot, so a concurrent transaction issuing a new ACTIVE prescription for
	// the same patient is invisible to it and surfaces as a unique violation on
	// the UPDATE itself, once that other transaction commits and this one stops
	// blocking on it -- which would otherwise abort the cancellation the user
	// actually asked for. Abandoning the reopen instead leaves the freed session
	// unreachable, which is the behaviour that existed before it was written and
	// is plainly better than refusing to cancel.
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prescription reopen savepoint: %w", err)
	}

	if err := reopenEachPrescription(ctx, savepoint, sortedUniqueIDs(prescriptionIDs)); err != nil {
		// A failed rollback still fails the caller -- the outer transaction is
		// aborted and could not commit anyway -- but the statement error is kept
		// alongside it, since it is the one that says what actually went wrong.
		if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("roll back prescription reopen savepoint: %w", rollbackErr))
		}

		// Only the race described above is forgiven. Anything else -- a dropped
		// connection, a broken query -- still fails the caller, so a genuine
		// fault cannot hide behind a cancellation that reports success.
		if isConcurrentActivePrescriptionConflict(err) {
			return nil
		}

		return err
	}

	if err := savepoint.Commit(ctx); err != nil {
		return fmt.Errorf("commit prescription reopen savepoint: %w", err)
	}

	return nil
}

func isConcurrentActivePrescriptionConflict(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}

	return pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintPrescriptionActivePerPatient
}

func reopenEachPrescription(ctx context.Context, tx pgx.Tx, prescriptionIDs []uuid.UUID) error {
	for _, prescriptionID := range prescriptionIDs {
		if _, err := tx.Exec(
			ctx,
			reopenPrescriptionQuery,
			prescriptionStatusActive,
			prescriptionID,
			prescriptionStatusCompleted,
			StatusConfirmed,
			StatusAbsent,
			StatusAttended,
		); err != nil {
			return fmt.Errorf("reopen prescription: %w", err)
		}
	}

	return nil
}

// The sort direction is arbitrary as long as every caller shares it; descending
// is used so that the newest prescription wins when one patient has two
// candidates, since domain.NewID mints UUIDv7 values that sort by creation time.
func sortedUniqueIDs(ids []uuid.UUID) []uuid.UUID {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int {
		return slices.Compare(b[:], a[:])
	})

	// Sorting puts duplicates next to each other, which is all Compact needs.
	return slices.Compact(sorted)
}

func fetchSlotRulesForUpdate(ctx context.Context, tx pgx.Tx, slotID uuid.UUID) (bool, int16, error) {
	var blocked bool
	var maxCapacity int16
	if err := tx.QueryRow(ctx, selectSlotForUpdateQuery, slotID).Scan(&blocked, &maxCapacity); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, 0, ErrInvalidAppointmentReference
		}
		return false, 0, fmt.Errorf("fetch slot rules for update: %w", err)
	}

	return blocked, maxCapacity, nil
}

func countConfirmedAppointmentsInSlot(ctx context.Context, tx pgx.Tx, slotID uuid.UUID) (int64, error) {
	var confirmedCount int64
	if err := tx.QueryRow(ctx, countConfirmedInSlotQuery, slotID, StatusConfirmed).Scan(&confirmedCount); err != nil {
		return 0, fmt.Errorf("count confirmed appointments in slot: %w", err)
	}

	return confirmedCount, nil
}

func hasOverlappingConfirmedAppointment(ctx context.Context, tx pgx.Tx, patientID, slotID uuid.UUID) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, hasOverlappingConfirmedQuery, patientID, slotID, StatusConfirmed).Scan(&exists); err != nil {
		return false, fmt.Errorf("check overlapping appointments: %w", err)
	}

	return exists, nil
}

func normalizeNotes(notes *string) *string {
	if notes == nil {
		return nil
	}

	trimmedNotes := strings.TrimSpace(*notes)
	if trimmedNotes == "" {
		return nil
	}

	return &trimmedNotes
}

func mapCreateAppointmentConstraintError(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return nil
	}

	if pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintAppointmentSlotPatientActive {
		return ErrMultipleActiveAppointmentsDetected
	}

	if pgErr.Code == pgErrForeignKeyViolation && isAppointmentForeignKeyConstraint(pgErr.ConstraintName) {
		return ErrInvalidAppointmentReference
	}

	return nil
}

func isAppointmentForeignKeyConstraint(name string) bool {
	switch name {
	case constraintAppointmentSlotFK,
		constraintAppointmentPatientFK,
		constraintAppointmentProfessionalFK,
		constraintAppointmentAssistantFK:
		return true
	default:
		return false
	}
}
