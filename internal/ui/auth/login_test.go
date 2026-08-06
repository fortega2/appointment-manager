package auth_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/ui/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	loginCaseRenderForm  = "render login form"
	loginCaseRenderError = "render login error message"
	submitES             = "Iniciar sesión"
	submitEN             = "Log In"
	//nolint:gosec // G101 false positive: a form label, not a credential.
	passwordLabelES = "Contraseña"
	passwordLabelEN = "Password"
)

func TestLogin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		locale   i18n.Locale
		submit   string
		password string
	}{
		{"spanish", i18n.LocaleES, submitES, passwordLabelES},
		{"english", i18n.LocaleEN, submitEN, passwordLabelEN},
	}

	for _, tt := range tests {
		t.Run(loginCaseRenderForm+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			require.NoError(t, auth.Login().Render(i18n.WithLocale(t.Context(), tt.locale), &buf))

			output := buf.String()

			assert.Contains(t, output, tt.submit)
			assert.Contains(t, output, tt.password)
			assert.Contains(t, output, `hx-post="/login"`)
			assert.Contains(t, output, `name="email"`)
			assert.Contains(t, output, `name="password"`)
		})
	}
}

func TestLoginError(t *testing.T) {
	t.Parallel()

	t.Run(loginCaseRenderError, func(t *testing.T) {
		t.Parallel()

		msg := "Invalid credentials"
		component := auth.LoginError(msg)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()

		assert.Contains(t, output, msg)
		assert.Contains(t, output, "<span>")
	})
}
