package appointment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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

	// Prescription status value used to mark a prescription as fully consumed.
	// Mirrors prescription.StatusCompleted; kept as a local literal so the
	// appointment repository stays decoupled from the prescription package,
	// consistent with how it queries the patient and slot tables directly.
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
	`
	selectAppointmentStatusQuery = `
		SELECT
			status
		FROM
			appointment
		WHERE
			id = $1
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
		return uuid.Nil, fmt.Errorf("begin create appointment transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockPatientForUpdate(ctx, tx, appoint.PatientID); err != nil {
		return uuid.Nil, err
	}

	prescriptionID, isLastSession, err := reserveActivePrescriptionSession(ctx, tx, appoint.PatientID)
	if err != nil {
		return uuid.Nil, err
	}

	if err := validateSlotForBooking(ctx, tx, appoint.PatientID, appoint.SlotID); err != nil {
		return uuid.Nil, err
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
			return uuid.Nil, mappedErr
		}
		return uuid.Nil, fmt.Errorf("create db appointment: %w", err)
	}

	// This booking consumed the last authorized session, so the prescription
	// is completed within the same transaction. This frees the partial unique
	// index so the patient can later be assigned a new active prescription.
	if isLastSession {
		if err := completePrescription(ctx, tx, prescriptionID); err != nil {
			return uuid.Nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit create appointment transaction: %w", err)
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

func (r *PostgresRepository) UpdateStatus(ctx context.Context, appointmentID uuid.UUID, newStatus, expectedStatus Status) error {
	result, err := r.pool.Exec(ctx, updateAppointmentStatusQuery, newStatus, appointmentID, expectedStatus)
	if err != nil {
		return fmt.Errorf("update appointment status: %w", err)
	}

	if result.RowsAffected() == 0 {
		_, readErr := r.readStatus(ctx, appointmentID)
		if readErr != nil {
			return readErr
		}

		return ErrAppointmentStatusChanged
	}

	return nil
}

func (r *PostgresRepository) readStatus(ctx context.Context, appointmentID uuid.UUID) (Status, error) {
	var status Status
	if err := r.pool.QueryRow(ctx, selectAppointmentStatusQuery, appointmentID).Scan(&status); err != nil {
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
		return uuid.Nil, false, err
	}

	consumedSessions, err := countConsumedSessions(ctx, tx, prescriptionID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if consumedSessions >= int64(totalSessions) {
		return uuid.Nil, false, ErrNoRemainingSessions
	}

	isLastSession := consumedSessions+1 >= int64(totalSessions)

	return prescriptionID, isLastSession, nil
}

func lockActivePrescriptionForUpdate(ctx context.Context, tx pgx.Tx, patientID uuid.UUID) (uuid.UUID, int16, error) {
	var prescriptionID uuid.UUID
	var totalSessions int16
	if err := tx.QueryRow(ctx, selectActivePrescriptionForUpdateQuery, patientID).Scan(&prescriptionID, &totalSessions); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, 0, ErrNoActivePrescription
		}
		return uuid.Nil, 0, fmt.Errorf("lock active prescription for update: %w", err)
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
