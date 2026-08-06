package home_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/ui/home"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	homeCaseRenderDashboard = "render home dashboard"
	welcomeES               = "Bienvenido a Appointment Manager"
	welcomeEN               = "Welcome to Appointment Manager"
	taglineES               = "Gestioná tus turnos"
	taglineEN               = "Manage your appointments"
	titleES                 = "<title>Panel"
	titleEN                 = "<title>Dashboard"
)

func TestHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		locale  i18n.Locale
		welcome string
		tagline string
		title   string
	}{
		{"spanish", i18n.LocaleES, welcomeES, taglineES, titleES},
		{"english", i18n.LocaleEN, welcomeEN, taglineEN, titleEN},
	}

	for _, tt := range tests {
		t.Run(homeCaseRenderDashboard+" in "+tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			require.NoError(t, home.Home().Render(i18n.WithLocale(t.Context(), tt.locale), &buf))

			output := buf.String()

			assert.Contains(t, output, tt.welcome)
			assert.Contains(t, output, tt.tagline)
			assert.Contains(t, output, tt.title)
		})
	}
}
