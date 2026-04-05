package appointment

import (
	"appointment-manager/internal/web"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	requestBodyMaxBytes int64 = 1 << 20

	defaultPage          = 1
	defaultLimit         = 20
	maxLimit             = 100
	queryParamsErrFormat = "%w: %q"

	pgErrUniqueViolation     = "23505"
	pgErrForeignKeyViolation = "23503"

	constraintAppointmentSlotPatientActive = "idx_appointment_slot_patient_active"
	constraintAppointmentSlotFK            = "fk_appointment_slot"
	constraintAppointmentPatientFK         = "fk_appointment_patient"
	constraintAppointmentProfessionalFK    = "fk_appointment_professional"
	constraintAppointmentAssistantFK       = "fk_appointment_assistant"

	confirmedStatusValue = StatusConfirmed
)

var (
	ErrNilLogger              = errors.New("logger cannot be nil")
	ErrNilDB                  = errors.New("database connection cannot be nil")
	ErrInvalidPage            = errors.New("invalid page")
	ErrInvalidLimit           = errors.New("invalid limit")
	ErrInvalidStatus          = errors.New("invalid status")
	ErrSlotIDRequired         = errors.New("slot id required")
	ErrInvalidSlotID          = errors.New("invalid slot id")
	ErrPatientIDRequired      = errors.New("patient id required")
	ErrInvalidPatientID       = errors.New("invalid patient id")
	ErrProfessionalIDRequired = errors.New("professional id required")
	ErrInvalidProfessionalID  = errors.New("invalid professional id")
	ErrAssistantIDRequired    = errors.New("assistant id required")
	ErrInvalidAssistantID     = errors.New("invalid assistant id")

	ErrMultipleActiveAppointmentsDetected = errors.New("patient cannot have multiple active appointments in overlapping time slots")
	ErrSlotBlocked                        = errors.New("slot is blocked")
	ErrSlotWithoutAvailability            = errors.New("slot has no available spots")
	ErrInvalidAppointmentReference        = errors.New("invalid appointment reference")
)

type Handler struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger, db *pgxpool.Pool) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if db == nil {
		return nil, ErrNilDB
	}

	return &Handler{
		db:     db,
		logger: logger,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/appointments", h.listAppointments())
	mux.Handle("POST /api/v1/appointments", h.createAppointment())
}

func (h *Handler) listAppointments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, limit, stQuery, err := h.parseQueryParams(r)
		if err != nil {
			web.WriteProblem(w, web.NewProblem(
				http.StatusBadRequest,
				web.ProblemTypeValidationFailed,
				"invalid list query parameters",
				r.URL.Path,
			))
			return
		}

		appointments, err := h.fetchAppointmentsFromDb(r.Context(), stQuery, limit, page)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "failed to fetch appointments from database", slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem("failed to fetch appointments", r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		if err := json.NewEncoder(w).Encode(appointments); err != nil {
			h.logger.ErrorContext(r.Context(), "failed to encode appointments response", slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem("failed to encode appointments response", r.URL.Path))
			return
		}
	}
}

func (h *Handler) parseQueryParams(r *http.Request) (int, int, Status, error) {
	pQuery := r.URL.Query().Get("page")
	lQuery := r.URL.Query().Get("limit")
	sQuery := r.URL.Query().Get("status")

	if pQuery == "" {
		pQuery = strconv.Itoa(defaultPage)
	}

	if lQuery == "" {
		lQuery = strconv.Itoa(defaultLimit)
	}

	if sQuery == "" {
		sQuery = fmt.Sprint(StatusConfirmed)
	}

	pageNum, err := strconv.Atoi(pQuery)
	if err != nil || pageNum < defaultPage {
		return 0, 0, 0, fmt.Errorf(queryParamsErrFormat, ErrInvalidPage, pQuery)
	}

	limitNum, err := strconv.Atoi(lQuery)
	if err != nil || limitNum < 1 || limitNum > maxLimit {
		return 0, 0, 0, fmt.Errorf(queryParamsErrFormat, ErrInvalidLimit, lQuery)
	}

	statusNum, err := strconv.Atoi(sQuery)
	if err != nil {
		return 0, 0, 0, fmt.Errorf(queryParamsErrFormat, ErrInvalidStatus, sQuery)
	}

	parsedStatus, err := parseStatus(statusNum)
	if err != nil {
		return 0, 0, 0, err
	}

	return pageNum, limitNum, parsedStatus, nil
}

func parseStatus(value int) (Status, error) {
	switch value {
	case int(StatusConfirmed):
		return StatusConfirmed, nil
	case int(StatusCancelled):
		return StatusCancelled, nil
	case int(StatusAbsent):
		return StatusAbsent, nil
	case int(StatusAttended):
		return StatusAttended, nil
	default:
		return 0, fmt.Errorf("%w: %d", ErrInvalidStatus, value)
	}
}

func (h *Handler) fetchAppointmentsFromDb(ctx context.Context, status Status, limit int, page int) ([]Appointment, error) {
	offset := (page - 1) * limit

	rows, err := h.db.Query(
		ctx,
		`SELECT
			id,
			slot_id,
			patient_id,
			professional_id,
			assistant_id,
			status
		FROM
			appointment
		WHERE
			status = $1
		ORDER BY
			created_at
		LIMIT
			$2
		OFFSET
			$3`,
		status,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("query appointments: %w", err)
	}
	defer rows.Close()

	appointments := make([]Appointment, 0, limit)
	for rows.Next() {
		var appt Appointment
		if err := rows.Scan(
			&appt.ID,
			&appt.SlotID,
			&appt.PatientID,
			&appt.ProfessionalID,
			&appt.AssistantID,
			&appt.Status,
		); err != nil {
			return nil, fmt.Errorf("scan appointment: %w", err)
		}
		appointments = append(appointments, appt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate appointments: %w", err)
	}

	return appointments, nil
}

type request struct {
	SlotID         string  `json:"slot_id"`
	PatientID      string  `json:"patient_id"`
	ProfessionalID string  `json:"professional_id"`
	AssistantID    string  `json:"assistant_id"`
	Notes          *string `json:"notes,omitempty"`
}

func (h *Handler) createAppointment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		problem := web.DecodeJSON(w, r, requestBodyMaxBytes, &req)
		if problem != nil {
			web.WriteProblem(w, *problem)
			return
		}

		if err := h.validateRequest(req); err != nil {
			web.WriteProblem(w, web.NewProblem(
				http.StatusUnprocessableEntity,
				web.ProblemTypeValidationFailed,
				err.Error(),
				r.URL.Path,
			))
			return
		}
		id, err := h.createAppointmentDb(r.Context(), req)
		if err != nil {
			switch {
			case errors.Is(err, ErrMultipleActiveAppointmentsDetected),
				errors.Is(err, ErrSlotBlocked),
				errors.Is(err, ErrSlotWithoutAvailability):
				web.WriteProblem(w, web.NewProblem(
					http.StatusConflict,
					web.ProblemTypeConflict,
					err.Error(),
					r.URL.Path,
				))
				return
			case errors.Is(err, ErrInvalidAppointmentReference):
				web.WriteProblem(w, web.NewProblem(
					http.StatusUnprocessableEntity,
					web.ProblemTypeValidationFailed,
					err.Error(),
					r.URL.Path,
				))
				return
			default:
				h.logger.ErrorContext(r.Context(), "failed to create appointment in database", slog.Any("error", err))
				web.WriteProblem(w, web.NewInternalServerProblem("failed to create appointment", r.URL.Path))
				return
			}
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		w.Header().Set("Location", "/api/v1/appointments/"+id.String())
		w.WriteHeader(http.StatusCreated)
	}
}

func (h *Handler) validateRequest(req request) error {
	if req.SlotID == "" {
		return ErrSlotIDRequired
	}
	if _, err := uuid.Parse(req.SlotID); err != nil {
		return ErrInvalidSlotID
	}

	if req.PatientID == "" {
		return ErrPatientIDRequired
	}
	if _, err := uuid.Parse(req.PatientID); err != nil {
		return ErrInvalidPatientID
	}

	if req.ProfessionalID == "" {
		return ErrProfessionalIDRequired
	}
	if _, err := uuid.Parse(req.ProfessionalID); err != nil {
		return ErrInvalidProfessionalID
	}

	if req.AssistantID == "" {
		return ErrAssistantIDRequired
	}
	if _, err := uuid.Parse(req.AssistantID); err != nil {
		return ErrInvalidAssistantID
	}

	return nil
}

func (h *Handler) createAppointmentDb(ctx context.Context, req request) (uuid.UUID, error) {
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin create appointment transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockPatientForUpdate(ctx, tx, req.PatientID); err != nil {
		return uuid.Nil, err
	}

	blocked, maxCapacity, err := fetchSlotRulesForUpdate(ctx, tx, req.SlotID)
	if err != nil {
		return uuid.Nil, err
	}
	if blocked {
		return uuid.Nil, ErrSlotBlocked
	}

	confirmedAppointments, err := countConfirmedAppointmentsInSlot(ctx, tx, req.SlotID)
	if err != nil {
		return uuid.Nil, err
	}
	if confirmedAppointments >= int64(maxCapacity) {
		return uuid.Nil, ErrSlotWithoutAvailability
	}

	hasOverlappingAppointment, err := hasOverlappingConfirmedAppointment(ctx, tx, req.PatientID, req.SlotID)
	if err != nil {
		return uuid.Nil, err
	}
	if hasOverlappingAppointment {
		return uuid.Nil, ErrMultipleActiveAppointmentsDetected
	}

	id := uuid.New()
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO appointment (
			id,
			slot_id,
			patient_id,
			professional_id,
			assistant_id,
			notes
		)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id,
		req.SlotID,
		req.PatientID,
		req.ProfessionalID,
		req.AssistantID,
		normalizeNotes(req.Notes),
	); err != nil {
		if mappedErr := mapCreateAppointmentConstraintError(err); mappedErr != nil {
			return uuid.Nil, mappedErr
		}
		return uuid.Nil, fmt.Errorf("create db appointment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit create appointment transaction: %w", err)
	}

	return id, nil
}

func lockPatientForUpdate(ctx context.Context, tx pgx.Tx, patientID string) error {
	var selectedPatientID uuid.UUID
	if err := tx.QueryRow(
		ctx,
		`SELECT
			id
		FROM
			patient
		WHERE
			id = $1
		FOR UPDATE`,
		patientID,
	).Scan(&selectedPatientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidAppointmentReference
		}
		return fmt.Errorf("lock patient for update: %w", err)
	}

	return nil
}

func fetchSlotRulesForUpdate(ctx context.Context, tx pgx.Tx, slotID string) (bool, int16, error) {
	var blocked bool
	var maxCapacity int16
	if err := tx.QueryRow(
		ctx,
		`SELECT
			blocked,
			max_capacity
		FROM
			slot
		WHERE
			id = $1
		FOR UPDATE`,
		slotID,
	).Scan(&blocked, &maxCapacity); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, 0, ErrInvalidAppointmentReference
		}
		return false, 0, fmt.Errorf("fetch slot rules for update: %w", err)
	}

	return blocked, maxCapacity, nil
}

func countConfirmedAppointmentsInSlot(ctx context.Context, tx pgx.Tx, slotID string) (int64, error) {
	var confirmedCount int64
	if err := tx.QueryRow(
		ctx,
		`SELECT
			COUNT(*)
		FROM
			appointment
		WHERE
			slot_id = $1
			AND status = $2`,
		slotID,
		confirmedStatusValue,
	).Scan(&confirmedCount); err != nil {
		return 0, fmt.Errorf("count confirmed appointments in slot: %w", err)
	}

	return confirmedCount, nil
}

func hasOverlappingConfirmedAppointment(ctx context.Context, tx pgx.Tx, patientID, slotID string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(
		ctx,
		`SELECT
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
					AND occupied_slot.date = target_slot.date
					AND occupied_slot.start_time < target_slot.end_time
					AND occupied_slot.end_time > target_slot.start_time
			)
		`,
		patientID,
		slotID,
		confirmedStatusValue,
	).Scan(&exists); err != nil {
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
