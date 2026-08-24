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
	Metrics  ResetMetrics
	BaseURL  string
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
	metrics  ResetMetrics
	baseURL  string
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

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	resetMetrics := cfg.Metrics
	if resetMetrics == nil {
		resetMetrics = noopResetMetrics{}
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
		metrics:  resetMetrics,
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
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

		if answered := h.refuseOverBudget(w, r, email, resetRefusedMsg); answered {
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
		// Rejected before the limiter so fumbling the confirmation field costs
		// nothing: the budget exists to protect the hash, not to punish typing.
		if answered := h.rejectWeakPassword(w, r, plain); answered {
			return
		}

		token := r.FormValue("token")
		if answered := h.refuseOverBudget(w, r, token, resetConfirmRefusedMsg); answered {
			return
		}

		// Verify before hashing so only a live link can occupy an Argon2 slot, and
		// Consume after it so a hash that never lands leaves the link redeemable.
		if err := h.tokens.Verify(r.Context(), token); err != nil {
			h.logger.WarnContext(r.Context(), failedVerifyResetMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusBadRequest, resetErrorTokenKey)

			return
		}

		hash, err := h.hasher.Hash(r.Context(), plain)
		if err != nil {
			if errors.Is(err, password.ErrTooManyConcurrentHashes) {
				renderError(w, r, h.logger, http.StatusServiceUnavailable, loginErrorBusyKey)
				return
			}

			h.logger.ErrorContext(r.Context(), failedHashPasswordMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, resetErrorFailedKey)

			return
		}

		assistantID, err := h.tokens.Consume(r.Context(), token)
		if err != nil {
			h.logger.WarnContext(r.Context(), failedConsumeResetMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusBadRequest, resetErrorTokenKey)

			return
		}

		// Past here the link is spent, so the copy stops promising a retry.
		//
		// Sessions go before the new hash: if the update then fails the account is
		// merely logged out, where the reverse order would leave a stolen session
		// alive. See ADR 0010.
		if _, err := h.sessions.DeleteByAssistant(r.Context(), assistantID.String()); err != nil {
			h.logger.ErrorContext(r.Context(), failedClearSessionsMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, resetErrorSpentKey)

			return
		}

		if err := h.repo.UpdatePasswordHash(r.Context(), assistantID, hash); err != nil {
			h.logger.ErrorContext(r.Context(), failedUpdatePasswordMsg, slog.Any("error", err))
			renderError(w, r, h.logger, http.StatusInternalServerError, resetErrorSpentKey)

			return
		}

		h.metrics.RecordPasswordResetCompleted()

		// No auto-login: a reset proves control of the mailbox, not of the account.
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	}
}

// refuseOverBudget reports whether it already answered the request. The account
// key is the email when asking for a link and the token when redeeming one --
// that route never sees an email, and without a budget an anonymous caller
// could hold the Argon2 slots the login shares.
func (h *ResetHandler) refuseOverBudget(w http.ResponseWriter, r *http.Request, key, refusedMsg string) bool {
	decision, _ := checkRateLimit(w, r, h.logger, h.limiter, key, refusedMsg)
	if decision.Allowed {
		return false
	}

	seconds := web.RetryAfterSeconds(decision.RetryAfter)
	renderErrorN(
		w, r, h.logger,
		http.StatusTooManyRequests,
		loginErrorRateLimitedKey,
		int(seconds),
		i18n.M{"seconds": seconds})

	return true
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
		h.metrics.RecordPasswordResetMailFailed()

		return fmt.Errorf("send reset mail: %w", err)
	}
	h.metrics.RecordPasswordResetMailSent()

	return nil
}

func (h *ResetHandler) renderPage(w http.ResponseWriter, r *http.Request, page templ.Component) {
	if err := page.Render(r.Context(), w); err != nil {
		h.logger.ErrorContext(r.Context(), renderResetPageMsg, slog.Any("error", err))
	}
}
