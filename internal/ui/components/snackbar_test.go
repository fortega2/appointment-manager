package components_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/ui/components"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	snackbarCaseSuccess = "render success type"
	snackbarCaseError   = "render error type"
	snackbarCaseInfo    = "render info type"
)

func TestSnackbar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		message       string
		snackbarType  components.SnackbarType
		expectedClass string
	}{
		{name: snackbarCaseSuccess, message: "Saved successfully", snackbarType: components.SnackbarSuccess, expectedClass: "snackbar-success"},
		{name: snackbarCaseError, message: "Failed to save", snackbarType: components.SnackbarError, expectedClass: "snackbar-error"},
		{name: snackbarCaseInfo, message: "Please note", snackbarType: components.SnackbarInfo, expectedClass: "snackbar-info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			component := components.Snackbar(tt.message, tt.snackbarType)

			var buf bytes.Buffer
			err := component.Render(t.Context(), &buf)
			require.NoError(t, err)

			output := buf.String()

			assert.Contains(t, output, tt.message)
			assert.Contains(t, output, tt.expectedClass)
		})
	}
}
