package slot

import (
	"appointment-manager/internal/professional"
	"appointment-manager/internal/ui/components"
	"appointment-manager/internal/ui/form"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type Handler struct {
	logger *slog.Logger
	repo   *Repository
	query  *Query
	pRepo  *professional.Repository
}

func NewHandler(logger *slog.Logger, repo *Repository, query *Query, pRepo *professional.Repository) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if repo == nil {
		return nil, ErrNilRepository
	}

	if query == nil {
		return nil, ErrNilQuery
	}

	return &Handler{
		logger: logger,
		repo:   repo,
		query:  query,
		pRepo:  pRepo,
	}, nil
}

func (h *Handler) RegisterUIHandlers(mux *http.ServeMux) {
	mux.Handle("GET /slots", h.showDashboardUIHandler())
	mux.Handle("GET /slots/new", h.showCreateFormUIHandler())
	mux.Handle("GET /slots/{id}/edit", h.showEditFormUIHandler())
	mux.Handle("POST /slots", h.createUIHandler())
	mux.Handle("PUT /slots/{id}", h.updateUIHandler())
}

func (h *Handler) showDashboardUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		dto, err := h.query.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list slots", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load slots", "slot.Query.List")
		}

		professionals, err := h.pRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load professionals", "professional.Query.List")
		}

		pDTO := make([]ProfessionalDTO, len(professionals))
		for i, p := range professionals {
			pDTO[i] = ProfessionalDTO{
				ID:        p.ID.String(),
				FirstName: p.FirstName,
				LastName:  p.LastName,
			}
		}

		if err := Dashboard(dto, pDTO).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "failed to render dashboard", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load dashboard", "Dashboard.Render")
		}
	}
}

func (h *Handler) createSnackbarError(ctx context.Context, w http.ResponseWriter, statusCode int, message, operation string) {
	w.WriteHeader(statusCode)
	if err := components.Snackbar(message, components.SnackbarError).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering snackbar", slog.Any("error", err), slog.String("package", "patient"), slog.String("operation", operation))
	}
}

func (h *Handler) showCreateFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		professionals, err := h.pRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals for form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load professionals", "pRepo.List")
			professionals = []professional.Professional{}
		}

		pDTO := make([]ProfessionalDTO, len(professionals))
		for i, p := range professionals {
			pDTO[i] = ProfessionalDTO{
				ID:        p.ID.String(),
				FirstName: p.FirstName,
				LastName:  p.LastName,
			}
		}

		if err := Form(ListDTO{}, form.MethodPost, "/slots", pDTO).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering slot create form", slog.Any("error", err))
		}
	}
}

func (h *Handler) showEditFormUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		idStr := r.PathValue("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			h.createSnackbarError(ctx, w, http.StatusBadRequest, "Invalid slot ID", "uuid.Parse")
			return
		}

		dto, err := h.query.GetByID(ctx, id)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to get slot by id", slog.Any("error", err), slog.String("id", idStr))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load slot", "query.GetByID")
			return
		}

		professionals, err := h.pRepo.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list professionals for edit form", slog.Any("error", err))
			professionals = []professional.Professional{}
		}

		pDTO := make([]ProfessionalDTO, len(professionals))
		for i, p := range professionals {
			pDTO[i] = ProfessionalDTO{
				ID:        p.ID.String(),
				FirstName: p.FirstName,
				LastName:  p.LastName,
			}
		}

		if err := Form(dto, form.MethodPut, "/slots/"+idStr, pDTO).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering slot edit form", slog.Any("error", err))
		}
	}
}

type formRequest struct {
	professionalID uuid.UUID
	date           time.Time
	startTime      time.Time
	endTime        time.Time
	maxCapacity    int16
	blocked        bool
}

func (h *Handler) parseForm(r *http.Request, w http.ResponseWriter) (*formRequest, error) {
	const requestBodyMaxBytes int64 = 1 << 20
	r.Body = http.MaxBytesReader(w, r.Body, requestBodyMaxBytes)
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("parse form: %w", err)
	}

	profIDStr := r.FormValue("professional_id")
	profID, err := uuid.Parse(profIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid professional_id: %w", err)
	}

	dateStr := r.FormValue("date")
	startTimeStr := r.FormValue("start_time")
	endTimeStr := r.FormValue("end_time")

	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		return nil, fmt.Errorf("load location: %w", err)
	}

	date, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %w", err)
	}

	startTime, err := time.ParseInLocation("2006-01-02 15:04", fmt.Sprintf("%s %s", dateStr, startTimeStr), loc)
	if err != nil {
		return nil, fmt.Errorf("invalid start_time format: %w", err)
	}

	endTime, err := time.ParseInLocation("2006-01-02 15:04", fmt.Sprintf("%s %s", dateStr, endTimeStr), loc)
	if err != nil {
		return nil, fmt.Errorf("invalid end_time format: %w", err)
	}

	maxCapStr := r.FormValue("max_capacity")
	maxCapInt, err := strconv.ParseInt(maxCapStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid max_capacity: %w", err)
	}

	blocked := r.FormValue("blocked") == "true" || r.FormValue("blocked") == "on"

	return &formRequest{
		professionalID: profID,
		date:           date,
		startTime:      startTime,
		endTime:        endTime,
		maxCapacity:    int16(maxCapInt),
		blocked:        blocked,
	}, nil
}

func (h *Handler) createUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		req, err := h.parseForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing slot create form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusBadRequest, "Failed to parse form data", "parseForm")
			return
		}

		s, err := NewSlot(req.professionalID, req.date, req.startTime, req.endTime, req.maxCapacity)
		if err != nil {
			h.logger.ErrorContext(ctx, "error creating slot from form data", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, err.Error(), "NewSlot")
			return
		}

		if err := h.repo.Create(ctx, s); err != nil {
			h.logger.ErrorContext(ctx, "failed to create slot", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to create slot", "repo.Create")
			return
		}

		h.renderUpdatedSlotsTable(ctx, w, "Slot created successfully")
	}
}

func (h *Handler) updateUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		idStr := r.PathValue("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			h.createSnackbarError(ctx, w, http.StatusBadRequest, "Invalid slot ID", "uuid.Parse")
			return
		}

		req, err := h.parseForm(r, w)
		if err != nil {
			h.logger.ErrorContext(ctx, "error parsing slot update form", slog.Any("error", err))
			h.createSnackbarError(ctx, w, http.StatusBadRequest, "Failed to parse form data", "parseForm")
			return
		}

		s, err := h.repo.GetByID(ctx, id)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to get slot by id for update", slog.Any("error", err), slog.String("id", idStr))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load slot data", "repo.GetByID")
			return
		}

		if err := s.Update(req.professionalID, req.date, req.startTime, req.endTime, req.maxCapacity, req.blocked); err != nil {
			h.logger.ErrorContext(ctx, "failed to validate slot update", slog.Any("error", err), slog.String("id", idStr))
			h.createSnackbarError(ctx, w, http.StatusUnprocessableEntity, err.Error(), "Slot.Update")
			return
		}

		if err := h.repo.Update(ctx, s); err != nil {
			h.logger.ErrorContext(ctx, "failed to update slot", slog.Any("error", err), slog.String("id", idStr))
			h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to update slot", "repo.Update")
			return
		}

		h.renderUpdatedSlotsTable(ctx, w, "Slot updated successfully")
	}
}

func (h *Handler) renderUpdatedSlotsTable(ctx context.Context, w http.ResponseWriter, successMsg string) {
	dto, err := h.query.List(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to list slots after operation", slog.Any("error", err))
		h.createSnackbarError(ctx, w, http.StatusInternalServerError, "Failed to load slots", "query.List")
		return
	}

	w.Header().Set("HX-Trigger", "close-modal")
	if err := components.Snackbar(successMsg, components.SnackbarSuccess).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering success snackbar after slot operation", slog.Any("error", err))
	}
	if err := Table(dto).Render(ctx, w); err != nil {
		h.logger.ErrorContext(ctx, "error rendering slots table after operation", slog.Any("error", err))
	}
}
