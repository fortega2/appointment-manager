package health

import (
	"appointment-manager/internal/web"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const (
	healthContentTypeHeader = "Content-Type"
	healthContentTypeJSON   = "application/json"
	problemTypeNotReady     = "/problems/not-ready"

	defaultReadinessTimeout = 300 * time.Millisecond
)

type CheckReady func(context.Context) error

type Handler struct {
	logger     *slog.Logger
	checkReady CheckReady
	timeout    time.Duration
}

type statusResponse struct {
	Status string `json:"status"`
}

func NewHandler(logger *slog.Logger, checkReady CheckReady, timeout time.Duration) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if checkReady == nil {
		return nil, ErrNilReadinessCheck
	}
	if timeout <= 0 {
		timeout = defaultReadinessTimeout
	}

	return &Handler{
		logger:     logger,
		checkReady: checkReady,
		timeout:    timeout,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /healthz", h.livenessHandler())
	mux.Handle("GET /readyz", h.readinessHandler())
}

func (h *Handler) livenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.writeStatusResponse(w, r, statusResponse{Status: "ok"}) {
			return
		}
	}
}

func (h *Handler) readinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
		defer cancel()

		if err := h.checkReady(ctx); err != nil {
			h.logger.ErrorContext(r.Context(), "readiness check failed", slog.Any("error", err))
			web.WriteProblem(w, web.NewProblem(
				http.StatusServiceUnavailable,
				problemTypeNotReady,
				"service not ready",
				r.URL.Path,
			))
			return
		}

		_ = h.writeStatusResponse(w, r, statusResponse{Status: "ok"})
	}
}

func (h *Handler) writeStatusResponse(w http.ResponseWriter, r *http.Request, response statusResponse) bool {
	body, err := json.Marshal(response)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "failed to encode health response", slog.Any("error", err))
		web.WriteProblem(w, web.NewInternalServerProblem("failed to encode health response", r.URL.Path))
		return false
	}
	w.Header().Set(healthContentTypeHeader, healthContentTypeJSON)
	_, err = w.Write(append(body, '\n'))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "failed to write health response", slog.Any("error", err))
	}

	return true
}
