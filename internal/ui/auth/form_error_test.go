package auth_test

import (
	"bytes"
	"testing"

	"appointment-manager/internal/ui/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const errorMessage = "Invalid credentials"

func TestFormError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, auth.FormError(errorMessage).Render(t.Context(), &buf))

	output := buf.String()

	assert.Contains(t, output, errorMessage)
	assert.Contains(t, output, "<span>")
}

// TestFormErrorEscapesItsInput pins that the message is not injected raw: it
// reaches this component from a catalog, but the catalog interpolates values.
func TestFormErrorEscapesItsInput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, auth.FormError("<script>alert(1)</script>").Render(t.Context(), &buf))

	assert.NotContains(t, buf.String(), "<script>")
}
