package auth_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/ui/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	loginCaseRenderForm  = "render login form"
	loginCaseRenderError = "render login error message"
)

func TestLogin(t *testing.T) {
	t.Parallel()

	t.Run(loginCaseRenderForm, func(t *testing.T) {
		t.Parallel()

		component := auth.Login()

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()

		assert.Contains(t, output, "Iniciar Sesión")
		assert.Contains(t, output, `hx-post="/login"`)
		assert.Contains(t, output, `name="email"`)
		assert.Contains(t, output, `name="password"`)
	})
}

func TestLoginError(t *testing.T) {
	t.Parallel()

	t.Run(loginCaseRenderError, func(t *testing.T) {
		t.Parallel()

		msg := "Credenciales inválidas"
		component := auth.LoginError(msg)

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()

		assert.Contains(t, output, msg)
		assert.Contains(t, output, "<span>")
	})
}
