package appointment

import (
	"appointment-manager/internal/ui/components"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

const (
	renderSnackbarErrMsg string = "error rendering snackbar"
)

type UIHandler struct {
	Handler

	query *Query
}

func NewUIHandler(logger *slog.Logger, service service, query *Query) (*UIHandler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if isNilService(service) {
		return nil, ErrNilService
	}

	if query == nil {
		return nil, ErrNilQuery
	}

	return &UIHandler{
		Handler{
			service: service,
			logger:  logger,
		},
		query,
	}, nil
}

func (h *UIHandler) RegisterUIHandlers(mux *http.ServeMux) {
	mux.Handle("GET /appointments", h.showDashboard())
	mux.Handle("POST /appointments", h.createUIHandler())
	mux.Handle("POST /appointments/{id}/attend", h.attendUIAppointment())
	mux.Handle("POST /appointments/{id}/cancel", h.cancelUIAppointment())
}

func (h *UIHandler) showDashboard() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		lg := h.logger.With(
			slog.String("package", "appointments"),
			slog.String("struct", "UIHandler"),
			slog.String("method", "showDashboard"),
		)

		appointments, err := h.query.List(ctx)
		if err != nil {
			lg.ErrorContext(ctx, "failed to list appointments", slog.Any("error", err))

			if dashErr := Dashboard([]List{}).Render(ctx, w); dashErr != nil {
				lg.ErrorContext(ctx, "error rendering appointment dashboard", slog.Any("error", dashErr))
			}
			return
		}

		if err := Dashboard(appointments).Render(ctx, w); err != nil {
			lg.ErrorContext(ctx, "error rendering appointment dashboard", slog.Any("error", err))
		}
	})
}

type uiRequest struct {
	SlotID         string
	PatientID      string
	ProfessionalID string
	AssistantID    string
	Notes          *string
}

func (h *UIHandler) createUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		lg := h.logger.With(
			slog.String("package", "appointments"),
			slog.String("struct", "UIHandler"),
			slog.String("method", "createUIHandler"),
		)

		req, err := h.parseForm(r, w)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusBadRequest, "Invalid form data")
			return
		}

		id, err := h.service.Create(ctx, CreateInput(*req))
		if err != nil {
			if !isCreateBusinessError(err) && !isCreateValidationError(err) {
				lg.ErrorContext(ctx, "failed to create appointment", slog.Any("error", err))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to create appointment")
			return
		}

		h.showSnackbar(ctx, lg, components.SnackbarSuccess, w, http.StatusOK, fmt.Sprintf("Appointment created with ID: %s", id))
		h.renderUpdatedAppointmentsTable(ctx, w, components.SnackbarSuccess, "Appointment created successfully")
	}
}

func (h *UIHandler) parseForm(r *http.Request, w http.ResponseWriter) (*uiRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, requestBodyMaxBytes)
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse form: %w", err)
	}

	slotID := r.FormValue("slot_id")
	patientID := r.FormValue("patient_id")
	professionalID := r.FormValue("professional_id")
	assistantID := r.FormValue("assistant_id")
	notes := r.FormValue("notes")
	if notes != "" {
		notes = strings.TrimSpace(notes)
	}

	return &uiRequest{
		SlotID:         slotID,
		PatientID:      patientID,
		ProfessionalID: professionalID,
		AssistantID:    assistantID,
		Notes:          &notes,
	}, nil
}

func (h *UIHandler) attendUIAppointment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		lg := h.logger.With(
			slog.String("package", "appointments"),
			slog.String("struct", "UIHandler"),
			slog.String("method", "attendUIAppointment"),
		)

		ID, err := parseAppointmentID(r)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusBadRequest, "Invalid appointment ID")
			return
		}

		if err := h.service.Attend(ctx, ID); err != nil {
			status, msg := resolveUIActionProblem(err)
			if status == http.StatusInternalServerError {
				lg.ErrorContext(ctx, "failed to mark appointment as attended", slog.String("appointment_id", ID.String()), slog.Any("error", err))
				msg = "Failed to mark appointment as attended"
			}
			h.renderUpdatedAppointmentsTable(ctx, w, components.SnackbarError, msg)
			return
		}

		h.renderUpdatedAppointmentsTable(ctx, w, components.SnackbarSuccess, "Appointment marked as attended successfully")
	}
}

func (h *UIHandler) cancelUIAppointment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		lg := h.logger.With(
			slog.String("package", "appointments"),
			slog.String("struct", "UIHandler"),
			slog.String("method", "cancelUIAppointment"),
		)

		ID, err := parseAppointmentID(r)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusBadRequest, "Invalid appointment ID")
			return
		}

		if err := h.service.Cancel(ctx, ID); err != nil {
			status, msg := resolveUIActionProblem(err)
			if status == http.StatusInternalServerError {
				lg.ErrorContext(ctx, "failed to cancel appointment", slog.String("appointment_id", ID.String()), slog.Any("error", err))
				msg = "Failed to cancel appointment"
			}
			h.renderUpdatedAppointmentsTable(ctx, w, components.SnackbarError, msg)
			return
		}

		h.renderUpdatedAppointmentsTable(ctx, w, components.SnackbarSuccess, "Appointment cancelled successfully")
	}
}

func (h *UIHandler) showSnackbar(ctx context.Context, lg *slog.Logger, kind components.SnackbarType, w http.ResponseWriter, status int, msg string) {
	if err := components.ShowSnackbar(ctx, kind, w, status, msg); err != nil {
		lg.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err), slog.String("operation", "ShowSnackbar"))
	}
}

func (h *UIHandler) renderUpdatedAppointmentsTable(ctx context.Context, w http.ResponseWriter, snackType components.SnackbarType, msg string) {
	lg := h.logger.With(
		slog.String("package", "appointments"),
		slog.String("struct", "UIHandler"),
		slog.String("method", "renderUpdatedAppointmentsTable"),
	)

	appointments, err := h.query.List(ctx)
	if err != nil {
		lg.ErrorContext(ctx, "failed to list appointments after operation", slog.Any("error", err))
		h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to load appointments")
		return
	}

	if err := components.Snackbar(msg, snackType).Render(ctx, w); err != nil {
		lg.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err))
	}
	if err := Table(appointments).Render(ctx, w); err != nil {
		lg.ErrorContext(ctx, "error rendering appointments table after operation", slog.Any("error", err))
	}
}

func resolveUIActionProblem(err error) (int, string) {
	switch {
	case errors.Is(err, ErrInvalidAppointmentReference):
		return http.StatusNotFound, "Appointment not found"
	case errors.Is(err, ErrAppointmentCannotAttendNow):
		return http.StatusUnprocessableEntity, "Appointment can only be attended during slot time"
	case errors.Is(err, ErrAppointmentCannotAttendWithStatus):
		return http.StatusConflict, "Appointment cannot be attended from current status"
	case errors.Is(err, ErrAppointmentCannotCancelWithStatus):
		return http.StatusConflict, "Appointment cannot be cancelled from current status"
	case errors.Is(err, ErrAppointmentStatusChanged):
		return http.StatusConflict, "Appointment status changed, please refresh"
	default:
		return http.StatusInternalServerError, "Failed to process request"
	}
}
