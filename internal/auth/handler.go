package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/password"
	"appointment-manager/internal/ratelimit"
	"appointment-manager/internal/session"
	"appointment-manager/internal/ui/auth"
	"appointment-manager/internal/web"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
)

const (
	maxBytesReader            int64  = 1 << 20
	failedGetAssistByEmailMsg string = "failed to get assistant by email"
	failedCreateSessionMsg    string = "failed to create session"
	failedDeleteSessionMsg    string = "failed to delete session"

	loginErrorFormKey string = "auth.error.form"
	loginErrorBusyKey string = "auth.error.busy"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	loginErrorCredentialsKey string = "auth.error.credentials"
	//nolint:gosec // G101 false positive: a message catalog key, not a credential.
	loginErrorPasswordKey    string = "auth.error.password"
	loginErrorSessionKey     string = "auth.error.session"
	loginErrorRateLimitedKey string = "auth.error.rate_limited"

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
	limiter       *ratelimit.Limiter
	isDevelopment bool
}

func NewHandler(
	logger *slog.Logger,
	store *session.Store,
	repo *assistant.PostgresRepository,
	pass *password.Argon2,
	limiter *ratelimit.Limiter,
	isDev bool,
) (*Handler, error) {
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
	if limiter == nil {
		return nil, ErrNilRateLimiter
	}

	return &Handler{
		logger:        logger,
		store:         store,
		repo:          repo,
		pass:          pass,
		limiter:       limiter,
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

		decision, addr := h.checkRateLimit(w, r, req.Email)
		if !decision.Allowed {
			web.WriteProblem(w, web.NewTooManyRequestsProblem(r.URL.Path))
			return
		}

		a, err := h.verifyCredentials(r.Context(), req.Email, req.Password)
		if err != nil {
			h.refundUnlessCredentialFailure(w, addr, req.Email, err)
			web.WriteProblem(w, loginProblem(err, r.URL.Path))
			return
		}

		h.recordLoginSuccess(w, addr, req.Email)

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

// checkRateLimit charges one attempt against the login allowance, writes the
// advisory headers and returns the decision along with the address it charged.
// It runs before verifyCredentials on purpose: a refused attempt then
// costs no Argon2 slot, which is the whole point of having it here rather than
// after the fact.
//
// A request whose address cannot be resolved shares one bucket with every other
// such request. That is a fallback for a case this deployment does not produce
// — the app is only reachable through a proxy that always sets the header — and
// collapsing them is safer than handing each an unlimited budget.
func (h *Handler) checkRateLimit(w http.ResponseWriter, r *http.Request, email string) (ratelimit.Decision, netip.Addr) {
	addr, _ := web.ClientIP(r)

	decision := h.limiter.Allow(addr, email)
	if !decision.Enforced() {
		return decision, addr
	}

	web.SetRateLimitHeaders(w.Header(), decision.Limit, decision.Remaining, decision.Reset)
	if !decision.Allowed {
		web.SetRetryAfter(w.Header(), decision.RetryAfter)
		// The address is logged but the email is not: it is attacker-controlled
		// and bounded only by maxBytesReader, so it has no place in a log line.
		h.logger.WarnContext(
			r.Context(),
			"login attempt refused for exceeding its allowance",
			slog.String("client_ip", addr.String()),
			slog.Int64("retry_after_seconds", web.RetryAfterSeconds(decision.RetryAfter)))
	}

	return decision, addr
}

// recordLoginSuccess credits a login that passed the password check, which puts
// the account's allowance back to full, and rewrites the headers so they do not
// keep advertising the budget the attempt was charged before it was credited.
// It takes the address checkRateLimit charged rather than resolving it again,
// so the two can never key on different buckets.
func (h *Handler) recordLoginSuccess(w http.ResponseWriter, addr netip.Addr, email string) {
	credited := h.limiter.RecordSuccess(addr, email)
	if credited.Enforced() {
		web.SetRateLimitHeaders(w.Header(), credited.Limit, credited.Remaining, credited.Reset)
	}
}

// refundUnlessCredentialFailure hands the attempt's token back when err says
// nothing about the credentials. A wrong password is the one failure the
// allowance is meant to ration; a lookup that never ran and a hashing slot that
// was never free are outages, and making a user sit out three minutes for one
// would be charging them for our own bad day.
func (h *Handler) refundUnlessCredentialFailure(w http.ResponseWriter, addr netip.Addr, email string, err error) {
	if errors.Is(err, errInvalidCredentials) {
		return
	}

	refunded := h.limiter.RecordAbandoned(addr, email)
	if refunded.Enforced() {
		web.SetRateLimitHeaders(w.Header(), refunded.Limit, refunded.Remaining, refunded.Reset)
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
			renderError(w, r, h.logger, http.StatusBadRequest, loginErrorFormKey)
			return
		}

		decision, addr := h.checkRateLimit(w, r, email)
		if !decision.Allowed {
			seconds := web.RetryAfterSeconds(decision.RetryAfter)
			renderErrorN(
				w, r, h.logger,
				http.StatusTooManyRequests,
				loginErrorRateLimitedKey,
				int(seconds),
				i18n.M{"seconds": seconds})

			return
		}

		a, err := h.verifyCredentials(r.Context(), email, pass)
		if err != nil {
			h.refundUnlessCredentialFailure(w, addr, email, err)

			switch {
			case errors.Is(err, password.ErrTooManyConcurrentHashes):
				renderError(w, r, h.logger, http.StatusServiceUnavailable, loginErrorBusyKey)
			case errors.Is(err, errInvalidCredentials):
				renderError(w, r, h.logger, http.StatusUnauthorized, loginErrorCredentialsKey)
			default:
				renderError(w, r, h.logger, http.StatusInternalServerError, loginErrorPasswordKey)
			}
			return
		}

		h.recordLoginSuccess(w, addr, email)

		sessionID, err := h.store.Create(r.Context(), a.ID.String())
		if err != nil {
			h.logger.ErrorContext(
				r.Context(),
				failedCreateSessionMsg,
				slog.String("assistant_id", a.ID.String()),
				slog.String("email", a.Email),
				slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, loginErrorSessionKey)
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
