package components

import (
	"context"
	"io"
	"net/http"

	"appointment-manager/internal/i18n"
)

// RenderSnackbar renders a snackbar whose copy is messageKey resolved in the
// request locale. Pass an i18n.M to fill %{name} placeholders.
//
// The key, rather than finished text, is the parameter on purpose: every caller
// was translating immediately before calling in, so the wrap belongs here.
func RenderSnackbar(ctx context.Context, w io.Writer, st SnackbarType, messageKey string, args ...any) error {
	return Snackbar(i18n.T(ctx, messageKey, args...), st).Render(ctx, w)
}

// ShowSnackbar writes a snackbar as the start of an htmx response. The snackbar
// is delivered out of band, so callers are expected to keep writing the content
// that belongs in the triggering element's target. Use ShowSnackbarOnly when the
// snackbar is the whole response.
func ShowSnackbar(ctx context.Context, st SnackbarType, w http.ResponseWriter, stCode int, messageKey string, args ...any) error {
	w.WriteHeader(stCode)

	return RenderSnackbar(ctx, w, st, messageKey, args...)
}

// ShowSnackbarOnly writes a response whose entire body is an out-of-band
// snackbar, with no content for the triggering element's target.
//
// HX-Reswap: none is required for that shape. The layout forces htmx to swap
// even on 4xx/5xx responses (see layout.Base), and once htmx has lifted the
// out-of-band snackbar out of the body nothing is left — so it would swap that
// emptiness into the target, blanking an hx-swap="innerHTML" element and
// deleting an hx-swap="outerHTML" one outright. The "none" strategy skips the
// target swap while still processing out-of-band content, so the snackbar
// still appears.
func ShowSnackbarOnly(ctx context.Context, st SnackbarType, w http.ResponseWriter, stCode int, messageKey string, args ...any) error {
	w.Header().Set("HX-Reswap", "none")

	return ShowSnackbar(ctx, st, w, stCode, messageKey, args...)
}
