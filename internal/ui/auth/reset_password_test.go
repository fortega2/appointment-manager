package auth_test

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/password"
	"appointment-manager/internal/ui/auth"
	"bytes"
	"strconv"
	"testing"

	"github.com/a-h/templ"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	resetToken = "a-reset-token"

	forgotTitleES = "Recuperar contraseña"
	forgotTitleEN = "Reset your password"
	resetTitleES  = "Elegí una contraseña nueva"
	resetTitleEN  = "Choose a new password"
	expiredES     = "El link ya no sirve"
	expiredEN     = "This link no longer works"
	forgotLinkES  = "¿Olvidaste tu contraseña?"
	forgotLinkEN  = "Forgot your password?"
)

func renderIn(t *testing.T, locale i18n.Locale, component templ.Component) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, component.Render(i18n.WithLocale(t.Context(), locale), &buf))

	return buf.String()
}

func TestForgotPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		locale i18n.Locale
		title  string
		name   string
	}{
		{name: "spanish", locale: i18n.LocaleES, title: forgotTitleES},
		{name: "english", locale: i18n.LocaleEN, title: forgotTitleEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderIn(t, tt.locale, auth.ForgotPassword())

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, `hx-post="/forgot-password"`)
			assert.Contains(t, output, `name="email"`)
			assert.NotContains(t, output, `name="password"`)
		})
	}
}

func TestResetPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		locale i18n.Locale
		title  string
		name   string
	}{
		{name: "spanish", locale: i18n.LocaleES, title: resetTitleES},
		{name: "english", locale: i18n.LocaleEN, title: resetTitleEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderIn(t, tt.locale, auth.ResetPassword(resetToken))

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, `hx-post="/reset-password"`)
			assert.Contains(t, output, `value="`+resetToken+`"`)
			assert.Contains(t, output, `name="password_confirmation"`)
			assert.Contains(t, output, `minlength="`+strconv.Itoa(password.MinLength)+`"`)
		})
	}
}

func TestResetPasswordExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		locale i18n.Locale
		title  string
		name   string
	}{
		{name: "spanish", locale: i18n.LocaleES, title: expiredES},
		{name: "english", locale: i18n.LocaleEN, title: expiredEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderIn(t, tt.locale, auth.ResetPasswordExpired())

			assert.Contains(t, output, tt.title)
			assert.Contains(t, output, `href="/forgot-password"`)
			assert.NotContains(t, output, `name="password"`)
		})
	}
}

// TestLoginOffersTheResetLink pins the only entry point the flow has.
func TestLoginOffersTheResetLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		locale i18n.Locale
		label  string
		name   string
	}{
		{name: "spanish", locale: i18n.LocaleES, label: forgotLinkES},
		{name: "english", locale: i18n.LocaleEN, label: forgotLinkEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderIn(t, tt.locale, auth.Login())

			assert.Contains(t, output, `href="/forgot-password"`)
			assert.Contains(t, output, tt.label)
		})
	}
}
