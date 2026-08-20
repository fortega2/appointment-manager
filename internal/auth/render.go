package auth

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/ui/auth"
	"log/slog"
	"net/http"
)

const renderFormErrorMsg string = "error rendering form error"

// renderError writes a form's inline error. It takes a catalog key rather than
// a message so the copy follows the request's locale, and args for the keys
// whose copy interpolates a value.
//
// Any header the response needs must already be set: this writes the status
// line, after which net/http discards further header writes.
func renderError(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	status int,
	messageKey string,
	args ...any,
) {
	renderMessage(w, r, logger, status, i18n.T(r.Context(), messageKey, args...))
}

// renderErrorN is renderError for a message whose wording depends on a count,
// so "1 second" does not render as "1 seconds".
func renderErrorN(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	status int,
	messageKey string,
	n int,
	args ...any,
) {
	renderMessage(w, r, logger, status, i18n.N(r.Context(), messageKey, n, args...))
}

func renderMessage(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, message string) {
	w.WriteHeader(status)
	if err := auth.FormError(message).Render(r.Context(), w); err != nil {
		logger.ErrorContext(r.Context(), renderFormErrorMsg, slog.Any("error", err))
	}
}
