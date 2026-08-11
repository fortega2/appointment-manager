package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/i18n"
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
	failedDeleteSessionMsg    string = "failed to delete session"

	loginErrorFormKey string = "auth.error.form"
	loginErrorBusyKey string = "auth.error.busy"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	loginErrorCredentialsKey string = "auth.error.credentials"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	loginErrorPasswordKey string = "auth.error.password"
	loginErrorSessionKey  string = "auth.error.session"

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

// RegisterHandlers mounts the login and logout routes. It takes a web.Mux so
// they can be registered behind a guard chain rather than on the bare mux.
func (h *Handler) RegisterHandlers(mux web.Mux) {
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

		sessionID, err := h.store.Create(r.Context(), a.ID.String())
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

// clearSession removes the session behind the request's cookie. A failure is
// logged rather than surfaced: the cookie is cleared either way, and the row
// left behind expires on its own.
func (h *Handler) clearSession(r *http.Request) {
	cookie, err := r.Cookie(session.CookieName)
	if err != nil {
		return
	}

	if err := h.store.Delete(r.Context(), cookie.Value); err != nil && !errors.Is(err, session.ErrSessionNotFound) {
		h.logger.ErrorContext(r.Context(), failedDeleteSessionMsg, slog.Any("error", err))
	}
}

func (h *Handler) logoutAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.clearSession(r)

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
		if _, cmpErr := h.pass.Compare(ctx, dummyHash, plainPassword); errors.Is(cmpErr, password.ErrTooManyConcurrentHashes) {
			return nil, cmpErr
		}

		return nil, errInvalidCredentials
	}

	ok, err := h.pass.Compare(ctx, a.PasswordHash, plainPassword)
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
			h.renderError(w, r, http.StatusBadRequest, loginErrorFormKey)
			return
		}

		a, err := h.verifyCredentials(r.Context(), email, pass)
		if err != nil {
			switch {
			case errors.Is(err, password.ErrTooManyConcurrentHashes):
				h.renderError(w, r, http.StatusServiceUnavailable, loginErrorBusyKey)
			case errors.Is(err, errInvalidCredentials):
				h.renderError(w, r, http.StatusUnauthorized, loginErrorCredentialsKey)
			default:
				h.renderError(w, r, http.StatusInternalServerError, loginErrorPasswordKey)
			}
			return
		}

		sessionID, err := h.store.Create(r.Context(), a.ID.String())
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				failedCreateSessionMsg,
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			h.renderError(w, r, http.StatusInternalServerError, loginErrorSessionKey)
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

// renderError writes the login form's inline error. It takes a catalog key
// rather than a message so the copy follows the request's locale.
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, messageKey string) {
	w.WriteHeader(status)
	if err := auth.LoginError(i18n.T(r.Context(), messageKey)).Render(r.Context(), w); err != nil {
		h.logger.ErrorContext(r.Context(), renderLoginErroMsg, slog.Any("error", err))
	}
}

func (h *Handler) logoutUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.clearSession(r)

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
