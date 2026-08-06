package middleware_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/ui/layout"
)

const (
	acceptLanguage = "Accept-Language"
	cookieHeader   = "Cookie"
	vary           = "Vary"
	spanishHeader  = "es-AR,es;q=0.9,en;q=0.8"
	englishHeader  = "en-US,en;q=0.9"
	frenchHeader   = "fr-FR"
	localeTitle    = "Dashboard"
	cookieEN       = middleware.LocaleCookie + "=en"
	cookieUnknown  = middleware.LocaleCookie + "=klingon"
	cookieEmpty    = middleware.LocaleCookie + "="
)

// TestMain loads the catalogs, without which i18n.WithLocale cannot resolve a
// locale and every assertion below would collapse to the fallback.
func TestMain(m *testing.M) {
	if err := i18n.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "loading catalogs: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// resolveThroughLocale reports the locale a request ends up rendering in, read
// from inside a handler wrapped by the middleware. cookie is the raw Cookie
// header, so a case can distinguish an absent cookie from an empty one.
func resolveThroughLocale(t *testing.T, def i18n.Locale, header, cookie string) i18n.Locale {
	t.Helper()

	var got i18n.Locale
	handler := middleware.Locale(def)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = i18n.FromContext(r.Context())
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set(acceptLanguage, header)
	}
	if cookie != "" {
		req.Header.Set(cookieHeader, cookie)
	}

	handler.ServeHTTP(httptest.NewRecorder(), req)

	return got
}

func TestLocaleResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		def    i18n.Locale
		header string
		cookie string
		want   i18n.Locale
	}{
		{
			name: "nothing to go on falls back to the default",
			def:  i18n.LocaleES,
			want: i18n.LocaleES,
		},
		{
			name: "nothing to go on honours a non-spanish default",
			def:  i18n.LocaleEN,
			want: i18n.LocaleEN,
		},
		{
			name:   "spanish browser is served spanish",
			def:    i18n.LocaleEN,
			header: spanishHeader,
			want:   i18n.LocaleES,
		},
		{
			name:   "english browser is served english",
			def:    i18n.LocaleES,
			header: englishHeader,
			want:   i18n.LocaleEN,
		},
		{
			name:   "unsupported language falls back to the default",
			def:    i18n.LocaleEN,
			header: frenchHeader,
			want:   i18n.LocaleEN,
		},
		{
			name:   "cookie overrides the browser",
			def:    i18n.LocaleES,
			header: spanishHeader,
			cookie: cookieEN,
			want:   i18n.LocaleEN,
		},
		{
			name:   "cookie overrides the default when no header is sent",
			def:    i18n.LocaleES,
			cookie: cookieEN,
			want:   i18n.LocaleEN,
		},
		{
			name:   "unparseable cookie degrades to the browser",
			def:    i18n.LocaleES,
			header: englishHeader,
			cookie: cookieUnknown,
			want:   i18n.LocaleEN,
		},
		{
			name:   "empty cookie degrades to the browser",
			def:    i18n.LocaleEN,
			header: spanishHeader,
			cookie: cookieEmpty,
			want:   i18n.LocaleES,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, resolveThroughLocale(t, tt.def, tt.header, tt.cookie))
		})
	}
}

func TestLocaleSetsVary(t *testing.T) {
	t.Parallel()

	handler := middleware.Locale(i18n.LocaleES)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	got := rec.Header().Get(vary)
	assert.Contains(t, got, "Cookie")
	assert.Contains(t, got, acceptLanguage)
}

func TestLocaleReachesTheLayout(t *testing.T) {
	t.Parallel()

	render := func(header string) string {
		var body strings.Builder
		handler := middleware.Locale(i18n.LocaleES)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			require.NoError(t, layout.Base(localeTitle, true).Render(r.Context(), &body))
		}))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.Header.Set(acceptLanguage, header)
		handler.ServeHTTP(httptest.NewRecorder(), req)

		return body.String()
	}

	assert.Contains(t, render(englishHeader), `<html lang="en">`)
	assert.Contains(t, render(spanishHeader), `<html lang="es">`)
}

func TestLocalePreservesExistingContextValues(t *testing.T) {
	t.Parallel()

	type key struct{}

	var got any
	handler := middleware.Locale(i18n.LocaleES)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Context().Value(key{})
	}))

	req := httptest.NewRequestWithContext(context.WithValue(t.Context(), key{}, "kept"), http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "kept", got)
}

func TestLocaleHandlesNilHandler(t *testing.T) {
	t.Parallel()

	handler := middleware.Locale(i18n.LocaleES)(nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/unmatched", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
