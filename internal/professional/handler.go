package professional

import (
	"appointment-manager/internal/ui/professional"
	"appointment-manager/internal/web"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	requestBodyMaxBytes int64 = 1 << 20
)

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
	mux.Handle("POST /api/v1/professionals", h.createHandler())
	mux.Handle("GET /api/v1/professionals", h.listHandler())
}

func (h *Handler) RegisterUIHandlers(mux *http.ServeMux) {
	mux.Handle("GET /professionals", h.showDashboardUIHandler())
}

type request struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
}

func (h *Handler) createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if problem := web.DecodeJSON(w, r, requestBodyMaxBytes, &req); problem != nil {
			web.WriteProblem(w, *problem)
			return
		}

		p, err := NewProfessional(req.FirstName, req.LastName, req.Phone)
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
			switch {
			case errors.Is(err, ErrInvalidProfessionalSpecialty):
				web.WriteProblem(w, web.NewProblem(
					http.StatusUnprocessableEntity,
					web.ProblemTypeValidationFailed,
					err.Error(),
					r.URL.Path,
				))
				return
			default:
				h.logger.ErrorContext(r.Context(), "failed to create professional", slog.Any("error", err))
				web.WriteProblem(w, web.NewInternalServerProblem("failed to create professional", r.URL.Path))
				return
			}
		}

		w.Header().Set("Location", "/api/v1/professionals/"+p.ID.String())
		w.WriteHeader(http.StatusCreated)
	}
}

func (h *Handler) listHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		professionals, err := h.repo.List(r.Context())
		if err != nil {
			h.logger.ErrorContext(r.Context(), "failed to list professionals", slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem("failed to list professionals", r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		if err := json.NewEncoder(w).Encode(professionals); err != nil {
			h.logger.ErrorContext(r.Context(), "failed to encode professionals response", slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem("failed to encode professionals response", r.URL.Path))
			return
		}
	}
}

func (h *Handler) showDashboardUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := professional.Dashboard().Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering professional dashboard", slog.Any("error", err))
		}
	}
}
