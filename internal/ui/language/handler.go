package language

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/web"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ErrNilLogger is returned by NewHandler when no logger is supplied.
var ErrNilLogger = errors.New("logger cannot be nil")

const (
	cookieMaxAge = int(365 * 24 * time.Hour / time.Second)

	headerHXRequest = "HX-Request"
	headerHXRefresh = "HX-Refresh"
)

// Handler serves the language switcher endpoint.
type Handler struct {
	logger        *slog.Logger
	isDevelopment bool
}

// NewHandler builds the language handler. isDev drops the Secure attribute from
// the cookie so the switcher also works over plain HTTP locally.
func NewHandler(logger *slog.Logger, isDev bool) (*Handler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	return &Handler{
		logger:        logger,
		isDevelopment: isDev,
	}, nil
}

// RegisterHandlers mounts the switcher. The route is action-style with the
// locale in the path and no request body, like the appointment transitions.
func (h *Handler) RegisterHandlers(mux web.Mux) {
	mux.Handle("POST /language/{locale}", h.setLanguageHandler())
}

func (h *Handler) setLanguageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requested := r.PathValue("locale")

		locale, ok := i18n.Parse(requested)
		if !ok {
			h.logger.WarnContext(r.Context(), "unsupported locale requested", slog.String("locale", requested))
			http.NotFound(w, r)
			return
		}

		//nolint:gosec // G124 false positive: Secure is dynamically !h.isDevelopment (true in prod, false only for local HTTP dev); HttpOnly/SameSite are already set.
		http.SetCookie(w, &http.Cookie{
			Name:     i18n.CookieName,
			Value:    string(locale),
			Path:     "/",
			MaxAge:   cookieMaxAge,
			Secure:   !h.isDevelopment,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		if r.Header.Get(headerHXRequest) != "" {
			w.Header().Set(headerHXRefresh, "true")
			w.WriteHeader(http.StatusNoContent)

			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}
