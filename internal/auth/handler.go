package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
	"appointment-manager/internal/ui/auth"
	"appointment-manager/internal/web"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	maxBytesReader            int64  = 1 << 20
	renderLoginErroMsg        string = "error rendering login error"
	failedGetAssistByEmailMsg string = "failed to get assistant by email"
	failedCreateSessionMsg    string = "failed to create session"
	serverBusyMsg             string = "Server is busy, please try again"
	incorrectCredentialsMsg   string = "Incorrect email or password"

	// dummyHash is compared against when an email is unknown, so that
	// path costs the same as a real verification and cannot be used to probe
	// which accounts exist.
	dummyHash string = "$argon2id$v=19$m=65536,t=3,p=2$P+GDBz2vGj467VpP0f5zWg$N/J6HjG8M1nJ8Jt3Vb4N/D1T1V7G7Q6H2C8P9W1L9Q"
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
	mux.Handle("POST /api/v1/auth/login", h.loginAPIHandler())
	mux.Handle("POST /api/v1/auth/logout", h.logoutAPIHandler())

	mux.Handle("GET /login", h.showLoginUIHandler())
	mux.Handle("POST /login", h.processLoginUIHandler())
	mux.Handle("POST /logout", h.logoutUIHandler())
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"` //nolint:gosec // Password is an input field required by the login API contract.
}

func (h *Handler) loginAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		problem := web.DecodeJSON(w, r, maxBytesReader, &req)
		if problem != nil {
			web.WriteProblem(w, *problem)
			return
		}

		a, err := h.verifyCredentials(r.Context(), req.Email, req.Password)
		if err != nil {
			web.WriteProblem(w, loginProblem(err, r.URL.Path))
			return
		}

		sessionID, err := h.store.Create(a.ID.String(), a.Email)
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				failedCreateSessionMsg,
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			web.WriteProblem(w, web.NewInternalServerProblem(failedCreateSessionMsg, r.URL.Path))
			return
		}

		//nolint:gosec // G124 false positive: Secure is dynamically !h.isDevelopment (true in prod, false only for local HTTP dev); HttpOnly/SameSite are already set.
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

func (h *Handler) logoutAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(session.CookieName); err == nil {
			h.store.Delete(cookie.Value)
		}

		//nolint:gosec // G124 false positive: Secure is dynamically !h.isDevelopment (true in prod, false only for local HTTP dev); HttpOnly/SameSite are already set.
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

func (h *Handler) showLoginUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := auth.Login().Render(ctx, w); err != nil {
			h.logger.ErrorContext(ctx, "error rendering login page", slog.Any("error", err))
		}
	}
}

// verifyCredentials looks up the assistant by email and compares the provided
// password against the stored hash. An unknown email still costs one Argon2id
// comparison against a dummy hash, so a caller cannot tell the two cases apart
// by timing. Errors are one of password.ErrTooManyConcurrentHashes,
// errInvalidCredentials, errCredentialLookupFailed or errPasswordCheckFailed.
func (h *Handler) verifyCredentials(ctx context.Context, email, plainPassword string) (*assistant.Assistant, error) {
	a, err := h.repo.GetByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, assistant.ErrAssistantNotFound) {
			h.logger.ErrorContext(ctx, failedGetAssistByEmailMsg, slog.String("email", email), slog.Any("error", err))
			// An infrastructure failure carries no account-existence signal to
			// mask, so skip the dummy hash instead of burning a hash slot.
			return nil, fmt.Errorf("%w: %w", errCredentialLookupFailed, err)
		}
		if _, cmpErr := h.pass.Compare(dummyHash, plainPassword); errors.Is(cmpErr, password.ErrTooManyConcurrentHashes) {
			return nil, cmpErr
		}

		return nil, errInvalidCredentials
	}

	ok, err := h.pass.Compare(a.PasswordHash, plainPassword)
	if err != nil {
		if errors.Is(err, password.ErrTooManyConcurrentHashes) {
			return nil, err
		}
		h.logger.ErrorContext(
			ctx,
			"failed to compare password hash",
			slog.String("assistant_id", a.ID.String()),
			slog.String("email", a.Email),
			slog.Any("error", err))

		return nil, fmt.Errorf("%w: %w", errPasswordCheckFailed, err)
	}
	if !ok {
		return nil, errInvalidCredentials
	}

	return a, nil
}

func (h *Handler) processLoginUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, pass, err := h.parseLoginForm(r, w)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "error parsing login form", slog.Any("error", err))
			h.renderError(w, r, "Error al procesar el formulario")
			return
		}

		a, err := h.verifyCredentials(r.Context(), email, pass)
		if err != nil {
			switch {
			case errors.Is(err, password.ErrTooManyConcurrentHashes):
				h.renderError(w, r, serverBusyMsg)
			case errors.Is(err, errPasswordCheckFailed):
				h.renderError(w, r, "Error verifying password")
			default:
				h.renderError(w, r, incorrectCredentialsMsg)
			}
			return
		}

		sessionID, err := h.store.Create(a.ID.String(), a.Email)
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				failedCreateSessionMsg,
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			h.renderError(w, r, "Error interno al crear sesión")
			return
		}
		//nolint:gosec // G124 false positive: Secure is dynamically !h.isDevelopment (true in prod, false only for local HTTP dev); HttpOnly/SameSite are already set.
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

func (h *Handler) parseLoginForm(r *http.Request, w http.ResponseWriter) (string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytesReader)

	email := r.FormValue("email")
	pass := r.FormValue("password")

	if email == "" || pass == "" {
		return "", "", errors.New("email and password are required")
	}

	return email, pass, nil
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, msg string) {
	if err := auth.LoginError(msg).Render(r.Context(), w); err != nil {
		h.logger.ErrorContext(r.Context(), renderLoginErroMsg, slog.Any("error", err))
	}
}

func (h *Handler) logoutUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(session.CookieName); err == nil {
			h.store.Delete(cookie.Value)
		}

		//nolint:gosec // G124 false positive: Secure is dynamically !h.isDevelopment (true in prod, false only for local HTTP dev); HttpOnly/SameSite are already set.
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
