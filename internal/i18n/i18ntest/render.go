// Package i18ntest provides helpers for view tests that assert on translated
// markup. It exists as its own package because the view tests are black box
// (patient_test, slot_test, ...) and so cannot share an unexported helper.
package i18ntest

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/i18n"
)

// Render renders component against a context carrying locale and returns the
// markup it produced.
func Render(t *testing.T, locale i18n.Locale, component templ.Component) string {
	t.Helper()

	var body strings.Builder
	require.NoError(t, component.Render(i18n.WithLocale(t.Context(), locale), &body))

	return body.String()
}
