package patient

import (
	"appointment-manager/internal/domain"
	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/ui/components"
	"appointment-manager/internal/ui/form"
	"appointment-manager/internal/web"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	requestBodyMaxBytes int64 = 1 << 20

	failedToCreatePatientMsg      string = "Failed to create patient"
	failedToCreatePatientLowerMsg string = "failed to create patient"
	missingIDInPathMsg            string = "missing patient id in path"
)

type Handler struct {
	logger              *slog.Logger
	repo                *Repository
	healthInsuranceRepo *healthinsurance.Repository
}

func NewHandler(logger *slog.Logger, repo *Repository, hiRepo *healthinsurance.Repository) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if repo == nil {
		return nil, ErrNilRepository
	}
	if hiRepo == nil {
		return nil, ErrNilHealthInsuranceRepository
	}

	return &Handler{
		logger:              logger,
		repo:                repo,
		healthInsuranceRepo: hiRepo,
	}, nil
}

func (h *Handler) RegisterHandlers(mux web.Mux) {
	mux.Handle("POST /api/v1/patients", h.createHandler())
}

func (h *Handler) RegisterUIHandlers(mux web.Mux) {
	mux.Handle("GET /patients", h.showDashboardUIHandler())
	mux.Handle("GET /patients/new", h.showCreateFormUIHandler())
	mux.Handle("PUT /patients/{id}", h.updateUIHandler())
	mux.Handle("GET /patients/{id}/edit", h.showEditFormUIHandler())
	mux.Handle("POST /patients", h.createUIHandler())
}

type request struct {
	FirstName       string  `json:"first_name"`
	LastName        string  `json:"last_name"`
	Phone           string  `json:"phone"`
	Email           string  `json:"email"`
	HealthInsurance int     `json:"health_insurance"`
	InsuranceNumber string  `json:"insurance_number"`
	ClinicalNotes   *string `json:"clinical_notes,omitempty"`
}

func (h *Handler) createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if problem := web.DecodeJSON(w, r, requestBodyMaxBytes, &req); problem != nil {
			web.WriteProblem(w, *problem)
			return
		}

		p, err := NewPatient(
			req.FirstName,
			req.LastName,
			req.Phone,
			req.Email,
			req.HealthInsurance,
			req.InsuranceNumber,
			req.ClinicalNotes,
		)
		if err != nil {
			web.WriteProblem(w, web.NewProblem(
				http.StatusUnprocessableEntity,
				web.ProblemTypeValidationFailed,
				err.Error(),
				r.URL.Path,
			))
			return
		}

		if err := h.repo.Create(r.Context(), p); err != nil {
			if errors.Is(err, ErrInvalidHealthInsurance) {
				web.WriteProblem(w, web.NewProblem(
					http.StatusUnprocessableEntity,
					web.ProblemTypeValidationFailed,
					err.Error(),
					r.URL.Path,
				))
				return
			}

			h.logger.ErrorContext(r.Context(), failedToCreatePatientLowerMsg, slog.Any("error", err))
			web.WriteProblem(w, web.NewProblem(
				http.StatusInternalServerError,
				web.ProblemTypeInternalServerError,
				failedToCreatePatientLowerMsg,
				r.URL.Path,
			))
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

func (h *Handler) showDashboardUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		patientsView, err := h.repo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list patients for dashboard", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyLoadPatients, "repo.List")
			return
		}

		if err := Dashboard(patientsView).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering patient dashboard", slog.Any("error", err))
		}
	}
}

func (h *Handler) showCreateFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		insurances, err := h.healthInsuranceRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list health insurances for form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyLoadInsurances, "healthInsuranceRepo.List")
			insurances = []healthinsurance.HealthInsurance{}
		}

		if err := Form(View{}, form.MethodPost, "/patients", insurances).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering patient create form", slog.Any("error", err))
		}
	}
}

func (h *Handler) showEditFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		patientID, err := h.parsePatientIDFromPath(r, w)
		if err != nil {
			return
		}

		view, err := h.repo.GetByID(ctx, patientID)
		if err != nil {
			if errors.Is(err, ErrPatientNotFound) {
				h.logger.WarnContext(ctx, "patient not found for edit form", slog.String("id", patientID.String()))
				return
			}
			h.logger.ErrorContext(ctx, "failed to get patient by id for edit form", slog.Any("error", err), slog.String("id", patientID.String()))
			return
		}

		insurances, err := h.healthInsuranceRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list health insurances for edit form", slog.Any("error", err))
			insurances = []healthinsurance.HealthInsurance{}
		}

		if err := Form(*view, form.MethodPut, "/patients/"+view.ID, insurances).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering patient edit form", slog.Any("error", err), slog.String("id", patientID.String()))
		}
	}
}

type formRequest struct {
	firstName       string
	lastName        string
	phone           string
	email           string
	healthInsurance int
	insuranceNumber string
	clinicalNotes   *string
}

func (h *Handler) createUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		req, err := h.parseForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing patient create form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusBadRequest, errKeyParseForm, "parseForm")
			return
		}

		if err := h.processPatientCreate(ctx, w, req); err != nil {
			return
		}

		h.renderUpdatedPatientsTable(ctx, w, msgKeyCreated)
	}
}

func (h *Handler) updateUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		patientID, err := h.parsePatientIDFromPath(r, w)
		if err != nil {
			return
		}

		req, err := h.parseForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing patient update form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusBadRequest, errKeyParseForm, "parseForm")
			return
		}

		if err := h.processPatientUpdate(ctx, w, patientID, req); err != nil {
			return
		}

		h.renderUpdatedPatientsTable(ctx, w, msgKeyUpdated)
	}
}

func (h *Handler) parsePatientIDFromPath(r *http.Request, w http.ResponseWriter) (uuid.UUID, error) {
	ctx := r.Context()
	pathValueID := r.PathValue("id")
	if pathValueID == "" {
		h.logger.WarnContext(ctx, missingIDInPathMsg)
		h.createSnackbarError(ctx, w, http.StatusBadRequest, errKeyMissingID, "missingIDInPath")
		return uuid.Nil, errors.New(missingIDInPathMsg)
	}

	patientID, err := domain.ParseID(pathValueID)
	if err != nil {
		h.logger.WarnContext(ctx, "invalid patient id in path", slog.Any("error", err), slog.String("id", pathValueID))
		h.createSnackbarError(ctx, w, http.StatusBadRequest, errKeyInvalidID, "invalidIDInPath")
		return uuid.Nil, err
	}
	return patientID, nil
}

func (h *Handler) parseForm(r *http.Request, w http.ResponseWriter) (*formRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, requestBodyMaxBytes)
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse form: %w", err)
	}

	firstName := r.FormValue("first_name")
	lastName := r.FormValue("last_name")
	phone := r.FormValue("phone")
	email := r.FormValue("email")
	insuranceNumber := r.FormValue("insurance_number")

	healthInsuranceStr := r.FormValue("health_insurance")
	healthInsurance, err := strconv.Atoi(healthInsuranceStr)
	if err != nil {
		return nil, fmt.Errorf("invalid health_insurance: %w", err)
	}

	clinicalNotesStr := r.FormValue("clinical_notes")
	var clinicalNotes *string
	if strings.TrimSpace(clinicalNotesStr) != "" {
		clinicalNotes = &clinicalNotesStr
	}

	return &formRequest{
		firstName:       firstName,
		lastName:        lastName,
		phone:           phone,
		email:           email,
		healthInsurance: healthInsurance,
		insuranceNumber: insuranceNumber,
		clinicalNotes:   clinicalNotes,
	}, nil
}

func (h *Handler) processPatientCreate(ctx context.Context, w http.ResponseWriter, req *formRequest) error {
	p, err := NewPatient(
		req.firstName,
		req.lastName,
		req.phone,
		req.email,
		req.healthInsurance,
		req.insuranceNumber,
		req.clinicalNotes,
	)
	if err != nil {
		h.logger.WarnContext(ctx, "invalid patient create form data", slog.Any("error", err))
		messageKey, args := validationErrorKey(err)
		h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, messageKey, "NewPatient", args)

		return err
	}

	if err := h.repo.Create(ctx, p); err != nil {
		if errors.Is(err, ErrInvalidHealthInsurance) {
			h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, errKeyInvalidInsurance, "repo.Create")
			return err
		}
		h.logger.ErrorContext(ctx, failedToCreatePatientLowerMsg, slog.Any("error", err))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyCreate, "repo.Create")
		return err
	}

	return nil
}

func (h *Handler) processPatientUpdate(ctx context.Context, w http.ResponseWriter, patientID uuid.UUID, req *formRequest) error {
	view, err := h.repo.GetByID(ctx, patientID)
	if err != nil {
		if errors.Is(err, ErrPatientNotFound) {
			h.logger.WarnContext(ctx, "patient not found for update", slog.String("id", patientID.String()))
			h.createSnackbarError(ctx, w, http.StatusNotFound, errKeyNotFound, "patientNotFound")
			return err
		}
		h.logger.ErrorContext(ctx, "failed to get patient by id for update", slog.Any("error", err), slog.String("id", patientID.String()))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyLoadPatient, "repo.GetByID")
		return err
	}

	p, err := view.ToPatient()
	if err != nil {
		h.logger.ErrorContext(ctx, "error converting patient view to patient model for update", slog.Any("error", err), slog.String("id", patientID.String()))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyProcessPatient, "view.ToPatient")
		return err
	}
	if err := p.Update(
		req.firstName,
		req.lastName,
		req.phone,
		req.email,
		req.healthInsurance,
		req.insuranceNumber,
		req.clinicalNotes,
	); err != nil {
		h.logger.WarnContext(ctx, "invalid patient update form data", slog.Any("error", err), slog.String("id", patientID.String()))
		messageKey, args := validationErrorKey(err)
		h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, messageKey, "Patient.Update", args)

		return err
	}

	if err := h.repo.Update(ctx, &p); err != nil {
		if errors.Is(err, ErrPatientNotFound) {
			h.createSnackbarError(ctx, w, http.StatusNotFound, errKeyNotFound, "patientNotFoundOnUpdate")
			return err
		}
		if errors.Is(err, ErrInvalidHealthInsurance) {
			h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, errKeyInvalidInsurance, "repo.Update")
			return err
		}
		h.logger.ErrorContext(ctx, "failed to update patient", slog.Any("error", err), slog.String("id", patientID.String()))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyUpdate, "repo.Update")
		return err
	}

	return nil
}

func (h *Handler) renderUpdatedPatientsTable(ctx context.Context, w http.ResponseWriter, successKey string) {
	patientsViews, err := h.repo.List(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to list patients after operation", slog.Any("error", err))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, errKeyLoadPatients, "repo.List")
		return
	}

	w.Header().Set("HX-Trigger", "close-modal")
	if err := components.Snackbar(i18n.T(ctx, successKey), components.SnackbarSuccess).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering success snackbar after patient operation", slog.Any("error", err))
	}
	if err := Table(patientsViews).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering patients table after operation", slog.Any("error", err))
	}
}

// createSnackbarError renders an error snackbar. It takes a catalog key rather
// than a message so the copy follows the request locale; args fill the key's
// placeholders, as the length-limit messages need.
func (h *Handler) createSnackbarError(ctx context.Context, w http.ResponseWriter, statusCode int, messageKey, operation string, args ...any) {
	if err := components.ShowSnackbarOnly(ctx, components.SnackbarError, w, statusCode, i18n.T(ctx, messageKey, args...)); err != nil {
		h.logger.ErrorContext(ctx, "error rendering snackbar", slog.Any("error", err), slog.String("package", "patient"), slog.String("operation", operation))
	}
}
