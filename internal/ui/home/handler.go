package home

import (
	"errors"
	"log/slog"
	"net/http"
)

var ErrNilLogger = errors.New("logger cannot be nil")

type Handler struct {
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	return &Handler{
		logger: logger,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /{$}", h.showHomeHandler())
}

func (h *Handler) showHomeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := Home().Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering home page", slog.Any("error", err))
		}
	}
}
