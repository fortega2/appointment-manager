package middleware_test

import (
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/ui/layout"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prescriptionsHref  = "/prescriptions"
	prescriptionsTitle = "Dashboard"
)

// renderThroughPrescriptions renders the layout from inside a handler wrapped
// by the middleware, so the assertion covers what a real request would see.
func renderThroughPrescriptions(t *testing.T, enabled bool) string {
	t.Helper()

	var body strings.Builder
	handler := middleware.Prescriptions(enabled)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		require.NoError(t, layout.Base(prescriptionsTitle, true).Render(r.Context(), &body))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	return body.String()
}

func TestPrescriptionsEnablesNavLink(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderThroughPrescriptions(t, true), prescriptionsHref)
}

func TestPrescriptionsDisablesNavLink(t *testing.T) {
	t.Parallel()

	assert.NotContains(t, renderThroughPrescriptions(t, false), prescriptionsHref)
}

func TestPrescriptionsPreservesExistingContextValues(t *testing.T) {
	t.Parallel()

	type key struct{}

	var got any
	handler := middleware.Prescriptions(true)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Context().Value(key{})
	}))

	req := httptest.NewRequestWithContext(context.WithValue(t.Context(), key{}, "kept"), http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "kept", got)
}

func TestPrescriptionsHandlesNilHandler(t *testing.T) {
	t.Parallel()

	handler := middleware.Prescriptions(true)(nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/unmatched", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
