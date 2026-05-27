package slot

import (
	"appointment-manager/internal/ui/components"
	"log/slog"
	"net/http"
)

type Handler struct {
	logger *slog.Logger
	repo   *Repository
	query  *Query
}

func NewHandler(logger *slog.Logger, repo *Repository, query *Query) (*Handler, error) {
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
	}, nil
}

func (h *Handler) RegisterUIHandlers(mux *http.ServeMux) {
	mux.Handle("GET /slots", h.showDashboardUIHandler())
}

func (h *Handler) showDashboardUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		dto, err := h.query.List(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to list slots", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			if err := components.Snackbar("Failed to load slots", components.SnackbarError).Render(ctx, w); err != nil {
				h.logger.ErrorContext(ctx, "failed to render error snackbar", slog.Any("error", err), slog.String("package", "slot"), slog.String("operation", "query.List"))
			}
		}

		if err := Dashboard(dto).Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "failed to render dashboard", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			if err := components.Snackbar("Failed to load dashboard", components.SnackbarError).Render(ctx, w); err != nil {
				h.logger.ErrorContext(ctx, "failed to render error snackbar", slog.Any("error", err), slog.String("package", "slot"), slog.String("operation", "Dashboard.Render"))
			}
		}
	}
}
