package patient

import (
	"appointment-manager/internal/web"
	"errors"
	"log/slog"
	"net/http"
)

const requestBodyMaxBytes int64 = 1 << 20

type Handler struct {
	logger *slog.Logger
	repo   *Repository
}

func NewHandler(logger *slog.Logger, repo *Repository) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if repo == nil {
		return nil, ErrNilRepository
	}

	return &Handler{
		logger: logger,
		repo:   repo,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/patients", h.createHandler())
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

			h.logger.ErrorContext(r.Context(), "failed to create patient", "error", err)
			web.WriteProblem(w, web.NewProblem(
				http.StatusInternalServerError,
				web.ProblemTypeInternalServerError,
				"failed to create patient",
				r.URL.Path,
			))
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}
