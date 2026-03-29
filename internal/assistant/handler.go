package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	failedToCreateAssistantMsg = "failed to create assistant"
)

type Repository interface {
	List(ctx context.Context) ([]Assistant, error)
	Get(ctx context.Context, id ID) (*Assistant, error)
	Create(ctx context.Context, assistant Assistant) (ID, error)
}

type Hasher interface {
	Hash(password string) (string, error)
}

type Handler struct {
	repo   Repository
	hasher Hasher
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger, repo Repository, hasher Hasher) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if repo == nil {
		return nil, ErrNilRepository
	}
	if hasher == nil {
		return nil, ErrNilPasswordHasher
	}

	return &Handler{
		repo:   repo,
		hasher: hasher,
		logger: logger,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/assistants", h.listHandler())
	mux.Handle("GET /api/v1/assistants/{id}", h.getHandler())
	mux.Handle("POST /api/v1/assistants", h.createHandler())
}

func (h *Handler) listHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assistants, err := h.repo.List(r.Context())
		if err != nil {
			h.logger.Error("failed to list assistants", slog.Any("error", err))
			http.Error(w, "failed to list assistants", http.StatusInternalServerError)
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
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

		w.Header().Set(contentTypeHeader, contentTypeJSON)
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

type request struct {
	Names      string `json:"names"`
	LastNames  string `json:"last_names"`
	Email      string `json:"email"`
	Passphrase string `json:"password"` //nolint:gosec // Request body field name required by API contract.
}

func (h *Handler) createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.logger.Error("failed to decode assistant request", slog.Any("error", err))
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		passwordHash, err := h.hasher.Hash(req.Passphrase)
		if err != nil {
			h.logger.Error("failed to hash assistant password", slog.Any("error", err))
			http.Error(w, failedToCreateAssistantMsg, http.StatusInternalServerError)
			return
		}

		assist, err := NewAssistant(req.Names, req.LastNames, req.Email, passwordHash)
		if err != nil {
			h.logger.Error("invalid assistant request", slog.Any("error", err))
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		id, err := h.repo.Create(r.Context(), *assist)
		if err != nil {
			h.logger.Error(failedToCreateAssistantMsg, slog.Any("error", err))
			http.Error(w, failedToCreateAssistantMsg, http.StatusInternalServerError)
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		w.Header().Set("Location", "/api/v1/assistants/"+id.String())
		w.WriteHeader(http.StatusCreated)
	}
}
