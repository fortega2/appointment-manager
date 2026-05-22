package home_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/ui/home"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	homeCaseRenderDashboard = "render home dashboard"
)

func TestHome(t *testing.T) {
	t.Parallel()

	t.Run(homeCaseRenderDashboard, func(t *testing.T) {
		t.Parallel()

		component := home.Home()

		var buf bytes.Buffer
		err := component.Render(t.Context(), &buf)
		require.NoError(t, err)

		output := buf.String()

		assert.Contains(t, output, "Welcome to Appointment Manager")
		assert.Contains(t, output, "Manage your appointments")
	})
}
