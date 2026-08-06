package layout_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/ui/layout"
)

const (
	switchToES = `hx-post="/language/es"`
	switchToEN = `hx-post="/language/en"`
	// The marker ctxi18n renders in place of copy when no catalog is loaded.
	missingLocaleMarker = "MISSING LOCALE"
)

// This package deliberately never loads the catalogs, so it is the honest place
// to prove a rendering path that skipped i18n.Load still ships real copy.
func TestBaseNeverRendersTheMissingLocaleMarker(t *testing.T) {
	t.Parallel()

	assert.NotContains(t, renderBase(t.Context(), t), missingLocaleMarker)
}

func TestLanguageSwitcherOffersOnlyTheInactiveLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		locale     i18n.Locale
		wantButton string
		wantInert  string
	}{
		{"spanish active offers english", i18n.LocaleES, switchToEN, switchToES},
		{"english active offers spanish", i18n.LocaleEN, switchToES, switchToEN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := renderBase(i18n.WithLocale(t.Context(), tt.locale), t)

			assert.Contains(t, body, tt.wantButton)
			assert.NotContains(t, body, tt.wantInert)
			assert.Contains(t, body, `aria-current="true"`)
		})
	}
}

// The switcher must reach the login page too, which renders unauthenticated.
func TestLanguageSwitcherRendersWhenUnauthenticated(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	require.NoError(t, layout.Base(dashboardTitle, false).Render(i18n.WithLocale(t.Context(), i18n.LocaleES), &body))

	assert.Contains(t, body.String(), switchToEN)
}
