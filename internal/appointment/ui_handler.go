package appointment

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/slot"
	"appointment-manager/internal/ui/components"
	"appointment-manager/internal/ui/form"
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

	query       *Query
	patientRepo *patient.Repository
	profRepo    *professional.Repository
	asstRepo    *assistant.PostgresRepository
	slotQuery   *slot.Query
}

func NewUIHandler(
	logger *slog.Logger,
	service service,
	query *Query,
	patientRepo *patient.Repository,
	profRepo *professional.Repository,
	asstRepo *assistant.PostgresRepository,
	slotQuery *slot.Query,
) (*UIHandler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if isNilService(service) {
		return nil, ErrNilService
	}

	if query == nil {
		return nil, ErrNilQuery
	}

	if patientRepo == nil {
		return nil, ErrNilPatientRepository
	}

	if profRepo == nil {
		return nil, ErrNilProfessionalRepo
	}

	if asstRepo == nil {
		return nil, ErrNilAssistantRepository
	}

	if slotQuery == nil {
		return nil, ErrNilSlotQuery
	}

	return &UIHandler{
		Handler{
			service: service,
			logger:  logger,
		},
		query,
		patientRepo,
		profRepo,
		asstRepo,
		slotQuery,
	}, nil
}

func (h *UIHandler) RegisterUIHandlers(mux *http.ServeMux) {
	mux.Handle("GET /appointments", h.showDashboard())
	mux.Handle("GET /appointments/new", h.showCreateFormUIHandler())
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

func (h *UIHandler) showCreateFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		lg := h.logger.With(
			slog.String("package", "appointments"),
			slog.String("struct", "UIHandler"),
			slog.String("method", "showCreateFormUIHandler"),
		)

		slotOpts, err := h.loadAvailableSlotOptions(ctx, lg)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to load available slots")
			return
		}

		patientOpts, err := h.loadPatientOptions(ctx, lg)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to load patients")
			return
		}

		profOpts, err := h.loadProfessionalOptions(ctx, lg)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to load professionals")
			return
		}

		asstOpts, err := h.loadAssistantOptions(ctx, lg)
		if err != nil {
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusInternalServerError, "Failed to load assistants")
			return
		}

		if err := Form(
			FormRequest{},
			form.MethodPost,
			"/appointments",
			slotOpts,
			patientOpts,
			profOpts,
			asstOpts,
		).Render(ctx, w); err != nil {
			lg.ErrorContext(ctx, "error rendering appointment create form", slog.Any("error", err))
		}
	}
}

func (h *UIHandler) loadAvailableSlotOptions(ctx context.Context, lg *slog.Logger) ([]SlotOptionDTO, error) {
	slots, err := h.slotQuery.ListAvailable(ctx)
	if err != nil {
		lg.ErrorContext(ctx, "failed to list available slots for form", slog.Any("error", err))
		return nil, err
	}

	options := make([]SlotOptionDTO, len(slots))
	for i, s := range slots {
		options[i] = SlotOptionDTO{
			ID:    s.ID,
			Label: fmt.Sprintf("%s %s-%s · Dr. %s", s.Date, s.StartTime, s.EndTime, s.ProfessionalName),
		}
	}
	return options, nil
}

func (h *UIHandler) loadPatientOptions(ctx context.Context, lg *slog.Logger) ([]PatientOptionDTO, error) {
	patients, err := h.patientRepo.List(ctx)
	if err != nil {
		lg.ErrorContext(ctx, "failed to list patients for form", slog.Any("error", err))
		return nil, err
	}

	options := make([]PatientOptionDTO, len(patients))
	for i, p := range patients {
		options[i] = PatientOptionDTO{
			ID:    p.ID,
			Label: fmt.Sprintf("%s %s", p.FirstName, p.LastName),
		}
	}
	return options, nil
}

func (h *UIHandler) loadProfessionalOptions(ctx context.Context, lg *slog.Logger) ([]ProfessionalOptionDTO, error) {
	professionals, err := h.profRepo.List(ctx)
	if err != nil {
		lg.ErrorContext(ctx, "failed to list professionals for form", slog.Any("error", err))
		return nil, err
	}

	options := make([]ProfessionalOptionDTO, len(professionals))
	for i, p := range professionals {
		options[i] = ProfessionalOptionDTO{
			ID:    p.ID.String(),
			Label: fmt.Sprintf("%s %s", p.FirstName, p.LastName),
		}
	}
	return options, nil
}

func (h *UIHandler) loadAssistantOptions(ctx context.Context, lg *slog.Logger) ([]AssistantOptionDTO, error) {
	assistants, err := h.asstRepo.List(ctx)
	if err != nil {
		lg.ErrorContext(ctx, "failed to list assistants for form", slog.Any("error", err))
		return nil, err
	}

	options := make([]AssistantOptionDTO, len(assistants))
	for i, a := range assistants {
		options[i] = AssistantOptionDTO{
			ID:    a.ID.String(),
			Label: fmt.Sprintf("%s %s", a.FirstName, a.LastName),
		}
	}
	return options, nil
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
			if renderErr := h.renderUpdatedAppointmentsTable(ctx, w); renderErr != nil {
				lg.ErrorContext(ctx, "error rendering appointments table after parse error", slog.Any("error", renderErr))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, http.StatusBadRequest, "Invalid form data")
			return
		}

		id, err := h.service.Create(ctx, CreateInput(*req))
		if err != nil {
			status, msg := resolveUICreateProblem(err)
			if status == http.StatusInternalServerError {
				lg.ErrorContext(ctx, "failed to create appointment", slog.Any("error", err))
			}
			if renderErr := h.renderUpdatedAppointmentsTable(ctx, w); renderErr != nil {
				lg.ErrorContext(ctx, "error rendering appointments table after failed create", slog.Any("error", renderErr))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, status, msg)
			return
		}

		lg.InfoContext(ctx, "appointment created", slog.String("appointment_id", id.String()))

		w.Header().Set("HX-Trigger", "close-modal")
		if err := components.Snackbar("Appointment created successfully", components.SnackbarSuccess).Render(ctx, w); err != nil {
			lg.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err))
		}
		if err := h.renderUpdatedAppointmentsTable(ctx, w); err != nil {
			lg.ErrorContext(ctx, "error rendering appointments table after create", slog.Any("error", err))
		}
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
			if err := h.renderUpdatedAppointmentsTable(ctx, w); err != nil {
				lg.ErrorContext(ctx, "error rendering appointments table after attend operation", slog.Any("error", err))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, status, msg)
			return
		}

		if err := h.renderUpdatedAppointmentsTable(ctx, w); err != nil {
			lg.ErrorContext(ctx, "error rendering appointments table after attend operation", slog.Any("error", err))
		}
		h.showSnackbar(ctx, lg, components.SnackbarSuccess, w, http.StatusOK, "Appointment marked as attended successfully")
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
			if err := h.renderUpdatedAppointmentsTable(ctx, w); err != nil {
				lg.ErrorContext(ctx, "error rendering appointments table after cancel operation", slog.Any("error", err))
			}
			h.showSnackbar(ctx, lg, components.SnackbarError, w, status, msg)
			return
		}

		if err := h.renderUpdatedAppointmentsTable(ctx, w); err != nil {
			lg.ErrorContext(ctx, "error rendering appointments table after cancel operation", slog.Any("error", err))
		}
		h.showSnackbar(ctx, lg, components.SnackbarSuccess, w, http.StatusOK, "Appointment cancelled successfully")
	}
}

func (h *UIHandler) showSnackbar(ctx context.Context, lg *slog.Logger, kind components.SnackbarType, w http.ResponseWriter, status int, msg string) {
	if err := components.ShowSnackbar(ctx, kind, w, status, msg); err != nil {
		lg.ErrorContext(ctx, renderSnackbarErrMsg, slog.Any("error", err), slog.String("operation", "ShowSnackbar"))
	}
}

func (h *UIHandler) renderUpdatedAppointmentsTable(ctx context.Context, w http.ResponseWriter) error {
	lg := h.logger.With(
		slog.String("package", "appointments"),
		slog.String("struct", "UIHandler"),
		slog.String("method", "renderUpdatedAppointmentsTable"),
	)

	appointments, err := h.query.List(ctx)
	if err != nil {
		lg.ErrorContext(ctx, "failed to list appointments after operation", slog.Any("error", err))
		return err
	}

	return Table(appointments).Render(ctx, w)
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

func resolveUICreateProblem(err error) (int, string) {
	switch {
	case errors.Is(err, ErrSlotIDRequired):
		return http.StatusBadRequest, "Slot is required"
	case errors.Is(err, ErrInvalidSlotID):
		return http.StatusBadRequest, "Invalid slot selected"
	case errors.Is(err, ErrPatientIDRequired):
		return http.StatusBadRequest, "Patient is required"
	case errors.Is(err, ErrInvalidPatientID):
		return http.StatusBadRequest, "Invalid patient selected"
	case errors.Is(err, ErrProfessionalIDRequired):
		return http.StatusBadRequest, "Professional is required"
	case errors.Is(err, ErrInvalidProfessionalID):
		return http.StatusBadRequest, "Invalid professional selected"
	case errors.Is(err, ErrAssistantIDRequired):
		return http.StatusBadRequest, "Assistant is required"
	case errors.Is(err, ErrInvalidAssistantID):
		return http.StatusBadRequest, "Invalid assistant selected"
	case errors.Is(err, ErrMultipleActiveAppointmentsDetected):
		return http.StatusConflict, "Patient already has an active appointment in that time slot"
	case errors.Is(err, ErrSlotBlocked):
		return http.StatusConflict, "Selected slot is blocked"
	case errors.Is(err, ErrSlotWithoutAvailability):
		return http.StatusConflict, "Selected slot has no available spots"
	case errors.Is(err, ErrNoActivePrescription):
		return http.StatusConflict, "Patient has no active prescription"
	case errors.Is(err, ErrNoRemainingSessions):
		return http.StatusConflict, "Patient's prescription has no remaining sessions"
	case errors.Is(err, ErrInvalidAppointmentReference):
		return http.StatusNotFound, "Referenced entity not found"
	default:
		return http.StatusInternalServerError, "Failed to create appointment"
	}
}
