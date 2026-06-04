package appointment

import (
	"appointment-manager/internal/ui/components"
	"context"
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

		_, err := h.query.List(ctx)
		if err != nil {
			lg.ErrorContext(ctx, "failed to list appointments", slog.Any("error", err))

			if snackbarErr := components.ShowSnackbar(ctx, components.SnackbarError, w, http.StatusInternalServerError, "Failed to load appointments"); snackbarErr != nil {
				lg.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", snackbarErr), slog.String("operation", "ShowSnackbar"))
			}
			return
		}

		// TODO: Render dashboard template with appointments data
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
		// TODO: Redirect to appointment details page or refresh dashboard
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
			if !isActionBusinessError(err) {
				lg.ErrorContext(ctx, "failed to mark appointment as attended", slog.String("appointment_id", ID.String()), slog.Any("error", err))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to mark appointment as attended")
			return
		}

		h.showSnackbar(ctx, lg, components.SnackbarSuccess, w, http.StatusOK, "Appointment marked as attended successfully")
		// TODO: Redirect to appointment details page or refresh dashboard
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
			if !isActionBusinessError(err) {
				lg.ErrorContext(ctx, "failed to cancel appointment", slog.String("appointment_id", ID.String()), slog.Any("error", err))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to cancel appointment")
			return
		}

		h.showSnackbar(ctx, lg, components.SnackbarSuccess, w, http.StatusOK, "Appointment cancelled successfully")
		// TODO: Redirect to appointment details page or refresh dashboard
	}
}

func (h *UIHandler) showSnackbar(ctx context.Context, lg *slog.Logger, kind components.SnackbarType, w http.ResponseWriter, status int, msg string) {
	if err := components.ShowSnackbar(ctx, kind, w, status, msg); err != nil {
		lg.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err), slog.String("operation", "ShowSnackbar"))
	}
}
