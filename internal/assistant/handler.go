package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type Repository interface {
	List(ctx context.Context) ([]Assistant, error)
	Get(ctx context.Context, id ID) (*Assistant, error)
}

type Handler struct {
	repo   Repository
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger, repo Repository) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if repo == nil {
		return nil, ErrNilRepository
	}

	return &Handler{
		repo:   repo,
		logger: logger,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("/api/v1/assistants", h.listHandler())
	mux.Handle("/api/v1/assistants/{id}", h.getHandler())
}

func (h *Handler) listHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assistants, err := h.repo.List(r.Context())
		if err != nil {
			h.logger.Error("failed to list assistants", slog.Any("error", err))
			http.Error(w, "failed to list assistants", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(assistants); err != nil {
			h.logger.Error("failed to encode assistants response", slog.Any("error", err))
			http.Error(w, "failed to encode assistants response", http.StatusInternalServerError)
			return
		}
	}
}

func (h *Handler) getHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqAssistID := r.PathValue("id")
		if reqAssistID == "" {
			http.Error(w, "assistant ID not found", http.StatusBadRequest)
			return
		}

		assistID, err := ParseID(reqAssistID)
		if err != nil {
			http.Error(w, "invalid assistant ID", http.StatusBadRequest)
			return
		}

		assistant, err := h.repo.Get(r.Context(), assistID)
		if err != nil {
			if errors.Is(err, ErrAssistantNotFound) {
				http.Error(w, "assistant not found", http.StatusNotFound)
				return
			}

			h.logger.Error(
				"failed to get assistant",
				slog.String("assistant_id", string(assistID)),
				slog.Any("error", err))
			http.Error(w, "failed to get assistant", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(assistant); err != nil {
			h.logger.Error(
				"failed to encode assistant response",
				slog.String("assistant_id", string(assistID)),
				slog.Any("error", err))
			http.Error(w, "failed to encode assistant response", http.StatusInternalServerError)
			return
		}
	}
}
