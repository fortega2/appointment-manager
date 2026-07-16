package layout_test

import (
	"appointment-manager/internal/ui/layout"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prescriptionsHref = "/prescriptions"
	dashboardTitle    = "Dashboard"
)

func renderBase(ctx context.Context, t *testing.T) string {
	t.Helper()

	var body strings.Builder
	require.NoError(t, layout.Base(dashboardTitle, true).Render(ctx, &body))

	return body.String()
}

func TestBaseShowsPrescriptionsLinkWhenEnabled(t *testing.T) {
	t.Parallel()

	body := renderBase(layout.WithPrescriptions(t.Context(), true), t)

	assert.Contains(t, body, prescriptionsHref)
}

func TestBaseHidesPrescriptionsLinkWhenDisabled(t *testing.T) {
	t.Parallel()

	body := renderBase(layout.WithPrescriptions(t.Context(), false), t)

	assert.NotContains(t, body, prescriptionsHref)
}

// A request that never passed through the prescriptions middleware must hide
// the link rather than render a dead one.
func TestBaseHidesPrescriptionsLinkWhenContextUnset(t *testing.T) {
	t.Parallel()

	body := renderBase(t.Context(), t)

	assert.NotContains(t, body, prescriptionsHref)
}

func TestBaseHidesPrescriptionsLinkWhenUnauthenticated(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	require.NoError(t, layout.Base(dashboardTitle, false).Render(layout.WithPrescriptions(t.Context(), true), &body))

	assert.NotContains(t, body.String(), prescriptionsHref)
}
