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

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	defaultPage          = 1
	defaultLimit         = 20
	maxLimit             = 100
	queryParamsErrFormat = "%w: %q"
)

var (
	ErrNilLogger     = errors.New("logger cannot be nil")
	ErrNilDB         = errors.New("database connection cannot be nil")
	ErrInvalidPage   = errors.New("invalid page")
	ErrInvalidLimit  = errors.New("invalid limit")
	ErrInvalidStatus = errors.New("invalid status")
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
