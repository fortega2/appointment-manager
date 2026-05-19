package professional

import (
	"appointment-manager/internal/web"
	"encoding/json"
	"errors"
	"fmt"
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
	mux.Handle("GET /professionals/new", h.showCreateFormUIHandler())
	mux.Handle("PATCH /professionals/{id}", h.updateUIHandler())
	mux.Handle("GET /professionals/{id}/edit", h.showEditFormUIHandler())
	mux.Handle("POST /professionals", h.createUIHandler())
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

		professionals, err := h.repo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals for dashboard", slog.Any("error", err))
			return
		}

		if err := Dashboard(professionalsToViews(professionals)).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering professional dashboard", slog.Any("error", err))
		}
	}
}

func (h *Handler) showCreateFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := Form(View{}, FormMethodPost, "/professionals").Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering professional create form", slog.Any("error", err))
		}
	}
}

func (h *Handler) showEditFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		pathValueID := r.PathValue("id")
		if pathValueID == "" {
			h.logger.WarnContext(ctx, "missing professional id in path")
			return
		}

		professionalID, err := ParseID(pathValueID)
		if err != nil {
			h.logger.WarnContext(ctx, "invalid professional id in path", slog.Any("error", err), slog.String("id", pathValueID))
			return
		}

		p, err := h.repo.GetByID(ctx, professionalID)
		if err != nil {
			if errors.Is(err, ErrProfessionalNotFound) {
				h.logger.WarnContext(ctx, "professional not found for edit form", slog.String("id", professionalID.String()))
				return
			}
			h.logger.ErrorContext(ctx, "failed to get professional by id for edit form", slog.Any("error", err), slog.String("id", professionalID.String()))
			return
		}

		if err := Form(professionalToView(p), FormMethodPatch, "/professionals/"+p.ID.String()).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering professional edit form", slog.Any("error", err), slog.String("id", professionalID.String()))
		}
	}
}

type createFormRequest struct {
	firstName string
	lastName  string
	phone     string
}

func (h *Handler) createUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		formRequest, err := h.parseCreateForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing professional create form", slog.Any("error", err))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		p, err := NewProfessional(formRequest.firstName, formRequest.lastName, formRequest.phone)
		if err != nil {
			h.logger.ErrorContext(ctx, "error creating professional from form data", slog.Any("error", err))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		if err := h.repo.Create(ctx, p); err != nil {
			if !errors.Is(err, ErrInvalidProfessionalSpecialty) {
				h.logger.ErrorContext(ctx, "failed to create professional", slog.Any("error", err))
				// TODO: Show a error message in the UI instead of just logging the error
				return
			}
		}

		professionals, err := h.repo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals after creating new one", slog.Any("error", err))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		w.Header().Set("HX-Trigger", "close-modal")
		if err := Table(professionalsToViews(professionals)).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering professionals table after creating new professional", slog.Any("error", err))
		}
	}
}

type updateFormRequest struct {
	firstName string
	lastName  string
	phone     string
	active    bool
}

func (h *Handler) updateUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		pathValueID := r.PathValue("id")
		if pathValueID == "" {
			h.logger.WarnContext(ctx, "missing professional id in path")
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		createFormRequest, err := h.parseUpdateForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing professional update form", slog.Any("error", err))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		professionalID, err := ParseID(pathValueID)
		if err != nil {
			h.logger.WarnContext(ctx, "invalid professional id in path", slog.Any("error", err), slog.String("id", pathValueID))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		p, err := h.repo.GetByID(ctx, professionalID)
		if err != nil {
			if errors.Is(err, ErrProfessionalNotFound) {
				h.logger.WarnContext(ctx, "professional not found for update", slog.String("id", professionalID.String()))
				// TODO: Show a error message in the UI instead of just logging the error
				return
			}
			h.logger.ErrorContext(ctx, "failed to get professional by id for update form", slog.Any("error", err), slog.String("id", professionalID.String()))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		if err := p.Update(createFormRequest.firstName, createFormRequest.lastName, createFormRequest.phone, createFormRequest.active); err != nil {
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		if err := h.repo.Update(ctx, p); err != nil {
			if errors.Is(err, ErrProfessionalNotFound) {
				// TODO: Show a error message in the UI instead of just logging the error
				return
			}
			h.logger.ErrorContext(ctx, "failed to update professional", slog.Any("error", err), slog.String("id", professionalID.String()))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		professionals, err := h.repo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals after updating one", slog.Any("error", err))
			// TODO: Show a error message in the UI instead of just logging the error
			return
		}

		w.Header().Set("HX-Trigger", "close-modal")
		if err := Table(professionalsToViews(professionals)).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering professionals table after updating professional", slog.Any("error", err))
		}
	}
}

func (h *Handler) parseCreateForm(r *http.Request, w http.ResponseWriter) (*createFormRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, requestBodyMaxBytes)
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse create form: %w", err)
	}

	firstName := r.FormValue("first_name")
	lastName := r.FormValue("last_name")
	phone := r.FormValue("phone")

	return &createFormRequest{
		firstName: firstName,
		lastName:  lastName,
		phone:     phone,
	}, nil
}

func (h *Handler) parseUpdateForm(r *http.Request, w http.ResponseWriter) (*updateFormRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, requestBodyMaxBytes)
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse update form: %w", err)
	}

	firstName := r.FormValue("first_name")
	lastName := r.FormValue("last_name")
	phone := r.FormValue("phone")
	activeStr := r.FormValue("active")
	active := activeStr == "true"

	return &updateFormRequest{
		firstName: firstName,
		lastName:  lastName,
		phone:     phone,
		active:    active,
	}, nil
}
