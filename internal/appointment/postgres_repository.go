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
			notes
		)
		VALUES ($1, $2, $3, $4, $5, $6)
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

	blocked, maxCapacity, err := fetchSlotRulesForUpdate(ctx, tx, appoint.SlotID)
	if err != nil {
		return uuid.Nil, err
	}
	if blocked {
		return uuid.Nil, ErrSlotBlocked
	}

	confirmedAppointments, err := countConfirmedAppointmentsInSlot(ctx, tx, appoint.SlotID)
	if err != nil {
		return uuid.Nil, err
	}
	if confirmedAppointments >= int64(maxCapacity) {
		return uuid.Nil, ErrSlotWithoutAvailability
	}

	hasOverlappingAppointment, err := hasOverlappingConfirmedAppointment(ctx, tx, appoint.PatientID, appoint.SlotID)
	if err != nil {
		return uuid.Nil, err
	}
	if hasOverlappingAppointment {
		return uuid.Nil, ErrMultipleActiveAppointmentsDetected
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
	); err != nil {
		if mappedErr := mapCreateAppointmentConstraintError(err); mappedErr != nil {
			return uuid.Nil, mappedErr
		}
		return uuid.Nil, fmt.Errorf("create db appointment: %w", err)
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
