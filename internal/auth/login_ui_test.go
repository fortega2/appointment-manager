package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/password"
)

const (
	loginUIPath      = "/login"
	formErrorES      = "Error al procesar el formulario"
	formErrorEN      = "Error processing the form"
	formContentType  = "application/x-www-form-urlencoded"
	headerContentTyp = "Content-Type"
)

// An empty form is the cheapest way to reach renderError without a database:
// parseLoginForm rejects it before any credential lookup happens.
func TestLoginUIErrorFollowsTheRequestLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		locale i18n.Locale
		want   string
	}{
		{"spanish", i18n.LocaleES, formErrorES},
		{"english", i18n.LocaleEN, formErrorEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			newTestHandler(t, password.NewArgon2(nil)).RegisterHandlers(mux)

			ctx := i18n.WithLocale(t.Context(), tt.locale)
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, loginUIPath, nil)
			req.Header.Set(headerContentTyp, formContentType)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
		})
	}
}
