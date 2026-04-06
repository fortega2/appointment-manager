package appointment

import (
	"appointment-manager/internal/domain"
	"appointment-manager/internal/web"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/google/uuid"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	requestBodyMaxBytes int64 = 1 << 20
)

type Handler struct {
	service service
	logger  *slog.Logger
}

type service interface {
	List(ctx context.Context, input ListInput) ([]Appointment, error)
	Create(ctx context.Context, input CreateInput) (uuid.UUID, error)
	Cancel(ctx context.Context, appointmentID uuid.UUID) error
	Attend(ctx context.Context, appointmentID uuid.UUID) error
}

func NewHandler(logger *slog.Logger, service service) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if isNilService(service) {
		return nil, ErrNilService
	}

	return &Handler{
		service: service,
		logger:  logger,
	}, nil
}

func isNilService(s service) bool {
	if s == nil {
		return true
	}

	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/appointments", h.listAppointments())
	mux.Handle("POST /api/v1/appointments", h.createAppointment())
	mux.Handle("POST /api/v1/appointments/{id}/cancel", h.cancelAppointment())
	mux.Handle("POST /api/v1/appointments/{id}/attend", h.attendAppointment())
}

func (h *Handler) listAppointments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appointments, err := h.service.List(r.Context(), ListInput{
			Page:   r.URL.Query().Get("page"),
			Limit:  r.URL.Query().Get("limit"),
			Status: r.URL.Query().Get("status"),
		})
		if err != nil {
			if isListValidationError(err) {
				web.WriteProblem(w, problemInvalidListQueryParams(r.URL.Path))
				return
			}

			h.logger.ErrorContext(r.Context(), "failed to list appointments", slog.Any("error", err))
			web.WriteProblem(w, problemListAppointments(r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		if err := json.NewEncoder(w).Encode(appointments); err != nil {
			h.logger.ErrorContext(r.Context(), "failed to encode appointments response", slog.Any("error", err))
			web.WriteProblem(w, problemEncodeAppointmentsResponse(r.URL.Path))
			return
		}
	}
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

		id, err := h.service.Create(r.Context(), CreateInput(req))
		if err != nil {
			if !isCreateBusinessError(err) && !isCreateValidationError(err) {
				h.logger.ErrorContext(r.Context(), "failed to create appointment", slog.Any("error", err))
			}
			web.WriteProblem(w, problemFromCreateError(err, r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		w.Header().Set("Location", "/api/v1/appointments/"+id.String())
		w.WriteHeader(http.StatusCreated)
	}
}

func (h *Handler) cancelAppointment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appointmentID, err := parseAppointmentID(r)
		if err != nil {
			web.WriteProblem(w, problemFromIDError(err, r.PathValue("id"), r.URL.Path))
			return
		}

		err = h.service.Cancel(r.Context(), appointmentID)
		if err != nil {
			if !isActionBusinessError(err) {
				h.logger.ErrorContext(
					r.Context(),
					"failed to cancel appointment",
					slog.String("appointment_id", appointmentID.String()),
					slog.Any("error", err),
				)
			}
			web.WriteProblem(w, problemFromCancelError(err, r.URL.Path))
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) attendAppointment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appointmentID, err := parseAppointmentID(r)
		if err != nil {
			web.WriteProblem(w, problemFromIDError(err, r.PathValue("id"), r.URL.Path))
			return
		}

		err = h.service.Attend(r.Context(), appointmentID)
		if err != nil {
			if !isActionBusinessError(err) {
				h.logger.ErrorContext(
					r.Context(),
					"failed to attend appointment",
					slog.String("appointment_id", appointmentID.String()),
					slog.Any("error", err),
				)
			}
			web.WriteProblem(w, problemFromAttendError(err, r.URL.Path))
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func isListValidationError(err error) bool {
	return errors.Is(err, ErrInvalidPage) ||
		errors.Is(err, ErrInvalidLimit) ||
		errors.Is(err, ErrInvalidStatus)
}

func isCreateValidationError(err error) bool {
	return errors.Is(err, ErrSlotIDRequired) ||
		errors.Is(err, ErrInvalidSlotID) ||
		errors.Is(err, ErrPatientIDRequired) ||
		errors.Is(err, ErrInvalidPatientID) ||
		errors.Is(err, ErrProfessionalIDRequired) ||
		errors.Is(err, ErrInvalidProfessionalID) ||
		errors.Is(err, ErrAssistantIDRequired) ||
		errors.Is(err, ErrInvalidAssistantID)
}

func isCreateBusinessError(err error) bool {
	return errors.Is(err, ErrMultipleActiveAppointmentsDetected) ||
		errors.Is(err, ErrSlotBlocked) ||
		errors.Is(err, ErrSlotWithoutAvailability) ||
		errors.Is(err, ErrInvalidAppointmentReference)
}

func isActionBusinessError(err error) bool {
	return errors.Is(err, ErrAppointmentCannotAttendNow) ||
		errors.Is(err, ErrAppointmentCannotAttendWithStatus) ||
		errors.Is(err, ErrAppointmentCannotCancelWithStatus) ||
		errors.Is(err, ErrAppointmentStatusChanged) ||
		errors.Is(err, ErrInvalidAppointmentReference)
}

func parseAppointmentID(r *http.Request) (uuid.UUID, error) {
	rawID := r.PathValue("id")
	if rawID == "" {
		return uuid.Nil, ErrAppointmentIDRequired
	}

	parsedID, err := domain.ParseID(rawID)
	if err != nil {
		return uuid.Nil, ErrInvalidAppointmentID
	}

	return parsedID, nil
}

func problemFromIDError(err error, rawID, path string) web.ProblemDetail {
	if errors.Is(err, ErrAppointmentIDRequired) {
		return problemAppointmentIDRequired(path)
	}

	return problemInvalidAppointmentID(rawID, path)
}

func isNilOrEmpty(raw string) bool {
	return raw == ""
}

func formatInvalidID(raw string) string {
	if isNilOrEmpty(raw) {
		return "invalid appointment ID"
	}

	return fmt.Sprintf("invalid appointment ID: %q", raw)
}
