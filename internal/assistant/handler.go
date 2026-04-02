package assistant

import (
	"appointment-manager/internal/domain"
	"appointment-manager/internal/web"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/google/uuid"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"

	createRequestBodyMaxBytes int64 = 1 << 20
)

type Handler struct {
	service service
	logger  *slog.Logger
}

type service interface {
	List(ctx context.Context) ([]Assistant, error)
	Get(ctx context.Context, id uuid.UUID) (*Assistant, error)
	Create(ctx context.Context, input CreateInput) (uuid.UUID, error)
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
			h.logger.ErrorContext(r.Context(), "failed to list assistants", slog.Any("error", err))
			web.WriteProblem(w, problemListAssistants(r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		if err := json.NewEncoder(w).Encode(assistants); err != nil {
			h.logger.ErrorContext(r.Context(), "failed to encode assistants response", slog.Any("error", err))
			web.WriteProblem(w, problemEncodeAssistantsResponse(r.URL.Path))
			return
		}
	}
}

func (h *Handler) getHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqAssistID := r.PathValue("id")
		if reqAssistID == "" {
			web.WriteProblem(w, problemAssistantIDNotFound(r.URL.Path))
			return
		}

		assistID, err := domain.ParseID(reqAssistID)
		if err != nil {
			web.WriteProblem(w, problemInvalidAssistantID(r.URL.Path))
			return
		}

		assistant, err := h.service.Get(r.Context(), assistID)
		if err != nil {
			if errors.Is(err, ErrAssistantNotFound) {
				web.WriteProblem(w, problemAssistantNotFound(r.URL.Path))
				return
			}

			h.logger.ErrorContext(
				r.Context(),
				"failed to get assistant",
				slog.String("assistant_id", assistID.String()),
				slog.Any("error", err))
			web.WriteProblem(w, problemGetAssistant(r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		if err := json.NewEncoder(w).Encode(assistant); err != nil {
			h.logger.ErrorContext(
				r.Context(),
				"failed to encode assistant response",
				slog.String("assistant_id", assistID.String()),
				slog.Any("error", err))
			web.WriteProblem(w, problemEncodeAssistantResponse(r.URL.Path))
			return
		}
	}
}

type request struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Password  string `json:"password"` //nolint:gosec // Request body field name required by API contract.
}

func (h *Handler) createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		problem := web.DecodeJSON(w, r, createRequestBodyMaxBytes, &req)
		if problem != nil {
			web.WriteProblem(w, *problem)
			return
		}

		id, err := h.service.Create(r.Context(), CreateInput(req))
		if err != nil {
			if !isValidationError(err) && !errors.Is(err, ErrEmailAlreadyExists) {
				h.logger.ErrorContext(r.Context(), "failed to create assistant", slog.Any("error", err))
			}
			web.WriteProblem(w, problemFromCreateError(err, r.URL.Path))
			return
		}

		w.Header().Set(contentTypeHeader, contentTypeJSON)
		w.Header().Set("Location", "/api/v1/assistants/"+id.String())
		w.WriteHeader(http.StatusCreated)
	}
}

func isValidationError(err error) bool {
	return errors.Is(err, ErrFirstNameRequired) ||
		errors.Is(err, ErrLastNameRequired) ||
		errors.Is(err, ErrEmailRequired) ||
		errors.Is(err, ErrEmailHasNoSign) ||
		errors.Is(err, ErrPasswordRequired)
}
