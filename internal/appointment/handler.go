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
)

var (
	ErrNilLogger = errors.New("logger cannot be nil")
	ErrNilDB     = errors.New("database connection cannot be nil")
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
			h.logger.ErrorContext(
				r.Context(),
				"failed to parse pagination parameters",
				slog.Any("error", err),
			)
			web.WriteProblem(w, web.NewInternalServerProblem("failed to parse pagination parameters", r.URL.Path))
			return
		}

		appointments, err := h.fetchAppointmentsFromDb(r.Context(), limit, page, stQuery)
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

func (h *Handler) parseQueryParams(r *http.Request) (uint, uint, Status, error) {
	pQuery := r.URL.Query().Get("page")
	lQuery := r.URL.Query().Get("limit")
	sQuery := r.URL.Query().Get("status")

	if pQuery == "" {
		pQuery = "1"
	}

	if lQuery == "" {
		lQuery = "20"
	}

	if sQuery == "" {
		sQuery = fmt.Sprint(StatusConfirmed)
	}

	pageNum, err := strconv.ParseUint(pQuery, 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}

	limitNum, err := strconv.ParseUint(lQuery, 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}

	statusNum, err := strconv.ParseUint(sQuery, 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}

	return uint(pageNum), uint(limitNum), Status(statusNum), nil
}

func (h *Handler) fetchAppointmentsFromDb(ctx context.Context, limit uint, page uint, status Status) ([]Appointment, error) {
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
			status = $3
		ORDER BY
			created_at
		LIMIT
			$1
		OFFSET
			$2`,
		limit,
		(page-1)*limit,
		status,
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
