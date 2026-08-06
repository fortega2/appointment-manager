package language_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/i18n"
	"appointment-manager/internal/ui/language"
)

const (
	pathES          = "/language/es"
	pathEN          = "/language/en"
	pathUnsupported = "/language/fr"
	headerHXRequest = "HX-Request"
	headerHXRefresh = "HX-Refresh"
	headerLocation  = "Location"
)

func newTestMux(t *testing.T, isDev bool) *http.ServeMux {
	t.Helper()

	h, err := language.NewHandler(slog.New(slog.DiscardHandler), isDev)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

// post drives the endpoint through the registered route, so the path wildcard
// is exercised the way a real request would exercise it.
func post(t *testing.T, mux *http.ServeMux, path string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
	if htmx {
		req.Header.Set(headerHXRequest, "true")
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

// localeCookie returns the lang cookie the response sets, or nil if it sets none.
func localeCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == i18n.CookieName {
			return cookie
		}
	}

	return nil
}

func TestNewHandlerRejectsNilLogger(t *testing.T) {
	t.Parallel()

	h, err := language.NewHandler(nil, true)

	require.ErrorIs(t, err, language.ErrNilLogger)
	assert.Nil(t, h)
}

func TestSetLanguageSetsCookie(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"spanish", pathES, string(i18n.LocaleES)},
		{"english", pathEN, string(i18n.LocaleEN)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cookie := localeCookie(t, post(t, newTestMux(t, true), tt.path, true))

			require.NotNil(t, cookie)
			assert.Equal(t, tt.want, cookie.Value)
		})
	}
}

func TestSetLanguageCookieAttributes(t *testing.T) {
	t.Parallel()

	cookie := localeCookie(t, post(t, newTestMux(t, false), pathEN, true))
	require.NotNil(t, cookie)

	assert.Equal(t, "/", cookie.Path)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Positive(t, cookie.MaxAge, "the choice must outlive the session")
	assert.True(t, cookie.Secure, "Secure is required outside development")
}

func TestSetLanguageDropsSecureInDevelopment(t *testing.T) {
	t.Parallel()

	cookie := localeCookie(t, post(t, newTestMux(t, true), pathEN, true))

	require.NotNil(t, cookie)
	assert.False(t, cookie.Secure)
}

func TestSetLanguageRefreshesForHTMX(t *testing.T) {
	t.Parallel()

	rec := post(t, newTestMux(t, true), pathEN, true)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "true", rec.Header().Get(headerHXRefresh))
}

func TestSetLanguageRedirectsWithoutHTMX(t *testing.T) {
	t.Parallel()

	rec := post(t, newTestMux(t, true), pathEN, false)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/", rec.Header().Get(headerLocation))
	assert.Empty(t, rec.Header().Get(headerHXRefresh))
}

func TestSetLanguageRejectsUnsupportedLocale(t *testing.T) {
	t.Parallel()

	rec := post(t, newTestMux(t, true), pathUnsupported, true)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Nil(t, localeCookie(t, rec), "a rejected locale must not be persisted")
}

func TestSetLanguageRejectsGET(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathEN, nil)
	rec := httptest.NewRecorder()
	newTestMux(t, true).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
