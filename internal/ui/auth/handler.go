package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	maxBytesReader     int64  = 1 << 20
	renderLoginErroMsg string = "error rendering login error"
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
	mux.Handle("GET /login", h.showLoginHandler())
	mux.Handle("POST /login", h.processLoginHandler())
	mux.Handle("POST /logout", h.logoutHandler())
}

func (h *Handler) showLoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := Login().Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering login page", slog.Any("error", err))
		}
	}
}

func (h *Handler) processLoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, pass, err := h.parseLoginForm(r, w)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "error parsing login form", slog.Any("error", err))
			h.renderError(w, r, "Error al procesar el formulario")
			return
		}

		a, err := h.repo.GetByEmail(r.Context(), email)
		if err != nil {
			if !errors.Is(err, assistant.ErrAssistantNotFound) {
				h.logger.ErrorContext(
					r.Context(),
					"failed to get assistant by email",
					slog.String("email", email),
					slog.Any("error", err))
			}
			h.renderError(w, r, "Email o contraseña incorrectos")
			return
		}
		ok, err := h.pass.Compare(a.PasswordHash, pass)
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				"failed to compare password hash",
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			h.renderError(w, r, "Error al verificar la contraseña")
			return
		}
		if !ok {
			h.renderError(w, r, "Email o contraseña incorrectos")
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
			h.renderError(w, r, "Error interno al crear sesión")
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

		w.Header().Set("HX-Redirect", "/")
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

		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) parseLoginForm(r *http.Request, w http.ResponseWriter) (string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytesReader)

	if err := r.ParseForm(); err != nil {
		return "", "", fmt.Errorf("failed to parse form: %w", err)
	}
	email := r.FormValue("email")
	pass := r.FormValue("password")

	return email, pass, nil
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, msg string) {
	if err := LoginError(msg).Render(r.Context(), w); err != nil {
		h.logger.ErrorContext(r.Context(), renderLoginErroMsg, slog.Any("error", err))
	}
}
