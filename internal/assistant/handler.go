package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	failedToCreateAssistantMsg = "failed to create assistant"
)

type Handler struct {
	service service
	logger  *slog.Logger
}

type service interface {
	List(ctx context.Context) ([]Assistant, error)
	Get(ctx context.Context, id ID) (*Assistant, error)
	Create(ctx context.Context, input CreateInput) (ID, error)
}

func NewHandler(logger *slog.Logger, service service) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if isNilService(service) {
		return nil, ErrNilService
	}

	return &Handler{
		service: service,
		logger:  logger,
	}, nil
}

func isNilService(s service) bool {
	if s == nil {
		return true
	}

	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/assistants", h.listHandler())
	mux.Handle("GET /api/v1/assistants/{id}", h.getHandler())
	mux.Handle("POST /api/v1/assistants", h.createHandler())
}

func (h *Handler) listHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assistants, err := h.service.List(r.Context())
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

		assistant, err := h.service.Get(r.Context(), assistID)
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
	Names     string `json:"names"`
	LastNames string `json:"last_names"`
	Email     string `json:"email"`
	Password  string `json:"password"` //nolint:gosec // Request body field name required by API contract.
}

func (h *Handler) createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.logger.Error("failed to decode assistant request", slog.Any("error", err))
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		id, err := h.service.Create(r.Context(), CreateInput(req))
		if err != nil {
			if isValidationError(err) {
				h.logger.Error("invalid assistant request", slog.Any("error", err))
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}

			h.logger.Error(failedToCreateAssistantMsg, slog.Any("error", err))
			http.Error(w, failedToCreateAssistantMsg, http.StatusInternalServerError)
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		w.Header().Set("Location", "/api/v1/assistants/"+id.String())
		w.WriteHeader(http.StatusCreated)
	}
}

func isValidationError(err error) bool {
	return errors.Is(err, ErrAssistantRequestNamesRequired) ||
		errors.Is(err, ErrAssistantRequestLastNamesRequired) ||
		errors.Is(err, ErrAssistantRequestEmailRequired) ||
		errors.Is(err, ErrAssistantRequestPasswordRequired)
}
