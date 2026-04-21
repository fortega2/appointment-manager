package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
	"appointment-manager/internal/web"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	createRequestBodyMaxBytes int64 = 1 << 20
)

type Handler struct {
	logger        *slog.Logger
	store         *session.Store
	repo          *assistant.PostgresRepository
	pass          *password.Argon2
	isDevelopment bool
}

func NewHandler(logger *slog.Logger, store *session.Store, repo *assistant.PostgresRepository, pass *password.Argon2, isDev bool) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if store == nil {
		return nil, ErrNilSessionStore
	}
	if repo == nil {
		return nil, ErrNilAssistantRepo
	}
	if pass == nil {
		return nil, ErrNilPasswordHasher
	}

	return &Handler{
		logger:        logger,
		store:         store,
		repo:          repo,
		pass:          pass,
		isDevelopment: isDev,
	}, nil
}

func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/auth/login", h.loginHandler())
	mux.Handle("POST /api/v1/auth/logout", h.logoutHandler())
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"` //nolint:gosec // Password is an input field required by the login API contract.
}

func (h *Handler) loginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		problem := web.DecodeJSON(w, r, createRequestBodyMaxBytes, &req)
		if problem != nil {
			web.WriteProblem(w, *problem)
			return
		}

		a, err := h.repo.GetByEmail(r.Context(), req.Email)
		if err != nil {
			if errors.Is(err, assistant.ErrAssistantNotFound) {
				web.WriteProblem(w, web.NewProblem(
					http.StatusUnauthorized,
					web.ProblemTypeUnauthorized,
					"email or password is incorrect",
					r.URL.Path,
				))
			} else {
				h.logger.ErrorContext(
					r.Context(),
					"failed to get assistant by email",
					slog.String("email", req.Email),
					slog.Any("error", err))
				web.WriteProblem(w, web.NewInternalServerProblem("failed to get assistant by email", r.URL.Path))
			}
			return
		}

		ok, err := h.pass.Compare(a.PasswordHash, req.Password)
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				"failed to compare password hash",
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem("failed to process the password", r.URL.Path))
			return
		}
		if !ok {
			web.WriteProblem(w, web.NewProblem(
				http.StatusUnauthorized,
				web.ProblemTypeUnauthorized,
				"email or password is incorrect",
				r.URL.Path,
			))
			return
		}

		sessionID, err := h.store.Create(a.ID.String(), a.Email)
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				"failed to create session",
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem("failed to create session", r.URL.Path))
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     session.CookieName,
			Value:    sessionID,
			Path:     "/",
			MaxAge:   int(session.SessionDuration / time.Second),
			Secure:   !h.isDevelopment,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(session.CookieName); err == nil {
			h.store.Delete(cookie.Value)
		}

		http.SetCookie(w, &http.Cookie{
			Name:     session.CookieName,
			Path:     "/",
			MaxAge:   -1,
			Secure:   !h.isDevelopment,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}
