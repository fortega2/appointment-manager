package auth

import (
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/mailer"
	"appointment-manager/internal/password"
	"appointment-manager/internal/passwordreset"
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
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"
)

// dispatchTimeout bounds the detached goroutine. mailer.Send reserves 30s of it.
const dispatchTimeout = 45 * time.Second

// Mailer and ResetRepo are declared at the consumer so the handler tests need
// neither a relay nor a database.
type Mailer interface {
	Send(ctx context.Context, msg mailer.Message) error
}

type ResetRepo interface {
	GetByEmail(ctx context.Context, email string) (*assistant.Assistant, error)
	UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error
}

// ResetHandlerConfig carries the handler's dependencies. Waiters is the group
// the shutdown drains, so a mail in flight is not cut off.
type ResetHandlerConfig struct {
	Logger   *slog.Logger
	Tokens   *passwordreset.Store
	Sessions *session.Store
	Repo     ResetRepo
	Hasher   *password.Argon2
	Mail     Mailer
	Limiter  *ratelimit.Limiter
	Waiters  *sync.WaitGroup
	BaseURL  string
	TokenTTL time.Duration
}

type ResetHandler struct {
	logger   *slog.Logger
	tokens   *passwordreset.Store
	sessions *session.Store
	repo     ResetRepo
	hasher   *password.Argon2
	mail     Mailer
	limiter  *ratelimit.Limiter
	waiters  *sync.WaitGroup
	baseURL  string
	tokenTTL time.Duration
}

func NewResetHandler(cfg ResetHandlerConfig) (*ResetHandler, error) {
	errs := make([]error, 0)

	if cfg.Logger == nil {
		errs = append(errs, ErrNilLogger)
	}
	if cfg.Tokens == nil {
		errs = append(errs, ErrNilResetTokenStore)
	}
	if cfg.Sessions == nil {
		errs = append(errs, ErrNilSessionStore)
	}
	if cfg.Repo == nil {
		errs = append(errs, ErrNilAssistantRepo)
	}
	if cfg.Hasher == nil {
		errs = append(errs, ErrNilPasswordHasher)
	}
	if cfg.Mail == nil {
		errs = append(errs, ErrNilMailer)
	}
	if cfg.Limiter == nil {
		errs = append(errs, ErrNilRateLimiter)
	}
	if cfg.Waiters == nil {
		errs = append(errs, ErrNilWaitGroup)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		errs = append(errs, ErrEmptyBaseURL)
	}
	if cfg.TokenTTL <= 0 {
		errs = append(errs, ErrNonPositiveTokenTTL)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return &ResetHandler{
		logger:   cfg.Logger,
		tokens:   cfg.Tokens,
		sessions: cfg.Sessions,
		repo:     cfg.Repo,
		hasher:   cfg.Hasher,
		mail:     cfg.Mail,
		limiter:  cfg.Limiter,
		waiters:  cfg.Waiters,
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		tokenTTL: cfg.TokenTTL,
	}, nil
}

// RegisterHandlers mounts the reset routes. They take no session: whoever walks
// this path cannot log in.
func (h *ResetHandler) RegisterHandlers(mux web.Mux) {
	mux.Handle("GET /forgot-password", h.showForgotPasswordHandler())
	mux.Handle("POST /forgot-password", h.requestResetHandler())
	mux.Handle("GET /reset-password", h.showResetPasswordHandler())
	mux.Handle("POST /reset-password", h.confirmResetHandler())
}

func (h *ResetHandler) showForgotPasswordHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.renderPage(w, r, auth.ForgotPassword())
	}
}

// requestResetHandler answers before doing any work, which is what makes the
// response time independent of whether the account exists.
func (h *ResetHandler) requestResetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytesReader)

		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			renderError(w, r, h.logger, http.StatusBadRequest, resetErrorEmailKey)
			return
		}

		if decision, _ := h.checkRateLimit(w, r, email); !decision.Allowed {
			seconds := web.RetryAfterSeconds(decision.RetryAfter)
			renderErrorN(
				w, r, h.logger,
				http.StatusTooManyRequests,
				resetErrorRateLimitedKey,
				int(seconds),
				i18n.M{"seconds": seconds})

			return
		}

		renderMessage(w, r, h.logger, http.StatusOK, i18n.T(r.Context(), resetNoticeSentKey))

		h.dispatch(r, email)
	}
}

func (h *ResetHandler) showResetPasswordHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The token rides in the URL, so it would otherwise leak to whatever the
		// page links out to.
		w.Header().Set("Referrer-Policy", "no-referrer")

		token := r.URL.Query().Get("token")
		if err := h.tokens.Verify(r.Context(), token); err != nil {
			h.renderPage(w, r, auth.ResetPasswordExpired())
			return
		}

		h.renderPage(w, r, auth.ResetPassword(token))
	}
}

func (h *ResetHandler) confirmResetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytesReader)
		w.Header().Set("Referrer-Policy", "no-referrer")

		plain := r.FormValue("password")
		if plain != r.FormValue("password_confirmation") {
			renderError(w, r, h.logger, http.StatusBadRequest, resetErrorMismatchKey)
			return
		}
		// Validating before Consume leaves the link usable, so a rejected
		// password costs a retry rather than a whole new mail.
		if answered := h.rejectWeakPassword(w, r, plain); answered {
			return
		}

		assistantID, err := h.tokens.Consume(r.Context(), r.FormValue("token"))
		if err != nil {
			h.logger.WarnContext(r.Context(), failedConsumeResetMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusBadRequest, resetErrorTokenKey)

			return
		}

		hash, err := h.hasher.Hash(r.Context(), plain)
		if err != nil {
			if errors.Is(err, password.ErrTooManyConcurrentHashes) {
				renderError(w, r, h.logger, http.StatusServiceUnavailable, loginErrorBusyKey)
				return
			}

			h.logger.ErrorContext(r.Context(), failedUpdatePasswordMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, resetErrorFailedKey)

			return
		}

		// Sessions go before the new hash: if the update then fails the account is
		// merely logged out, where the reverse order would leave a stolen session
		// alive. See ADR 0010.
		if _, err := h.sessions.DeleteByAssistant(r.Context(), assistantID.String()); err != nil {
			h.logger.ErrorContext(r.Context(), failedClearSessionsMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, resetErrorFailedKey)

			return
		}

		if err := h.repo.UpdatePasswordHash(r.Context(), assistantID, hash); err != nil {
			h.logger.ErrorContext(r.Context(), failedUpdatePasswordMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, resetErrorFailedKey)

			return
		}

		// No auto-login: a reset proves control of the mailbox, not of the account.
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	}
}

// rejectWeakPassword reports whether it already answered the request.
func (h *ResetHandler) rejectWeakPassword(w http.ResponseWriter, r *http.Request, plain string) bool {
	switch err := password.Validate(plain); {
	case errors.Is(err, password.ErrPasswordTooShort):
		renderError(w, r, h.logger, http.StatusBadRequest, resetErrorTooShortKey, i18n.M{"count": password.MinLength})
	case errors.Is(err, password.ErrPasswordTooLong):
		renderError(w, r, h.logger, http.StatusBadRequest, resetErrorTooLongKey, i18n.M{"count": password.MaxLength})
	default:
		return false
	}

	return true
}

// dispatch detaches the work from the request. WithoutCancel keeps the locale
// and trace ids while dropping the cancellation; the timeout is its own,
// because nothing else would ever end this context.
func (h *ResetHandler) dispatch(r *http.Request, email string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), dispatchTimeout)

	h.waiters.Go(func() {
		defer cancel()
		if err := h.sendResetLink(ctx, email); err != nil {
			h.logger.ErrorContext(ctx, failedDispatchResetMsg, slog.Any("error", err))
		}
	})
}

// sendResetLink treats an unknown address as success: nothing to do is not a
// failure, and the caller has already answered either way.
func (h *ResetHandler) sendResetLink(ctx context.Context, email string) error {
	a, err := h.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, assistant.ErrAssistantNotFound) {
			return nil
		}

		return fmt.Errorf("look up assistant: %w", err)
	}

	token, err := h.tokens.Create(ctx, a.ID)
	if err != nil {
		return fmt.Errorf("create reset token: %w", err)
	}

	message, err := h.resetMessage(ctx, a.Email, token)
	if err != nil {
		return err
	}

	if err := h.mail.Send(ctx, message); err != nil {
		return fmt.Errorf("send reset mail: %w", err)
	}

	return nil
}

func (h *ResetHandler) checkRateLimit(
	w http.ResponseWriter,
	r *http.Request,
	email string,
) (ratelimit.Decision, netip.Addr) {
	addr, _ := web.ClientIP(r)

	decision := h.limiter.Allow(addr, email)
	if !decision.Enforced() {
		return decision, addr
	}

	web.SetRateLimitHeaders(w.Header(), decision.Limit, decision.Remaining, decision.Reset)
	if !decision.Allowed {
		web.SetRetryAfter(w.Header(), decision.RetryAfter)
		h.logger.WarnContext(
			r.Context(),
			"password reset refused for exceeding its allowance",
			slog.String("client_ip", addr.String()),
			slog.Int64("retry_after_seconds", web.RetryAfterSeconds(decision.RetryAfter)))
	}

	return decision, addr
}

func (h *ResetHandler) renderPage(w http.ResponseWriter, r *http.Request, page templ.Component) {
	if err := page.Render(r.Context(), w); err != nil {
		h.logger.ErrorContext(r.Context(), renderResetPageMsg, slog.Any("error", err))
	}
}
