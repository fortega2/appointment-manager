package middleware

import (
	"net/http"
	"strings"

	"appointment-manager/internal/i18n"
)

const (
	// LocaleCookie names the cookie the language switcher writes and this
	// middleware reads back.
	LocaleCookie = "lang"

	headerAcceptLanguage = "Accept-Language"
	headerCookie         = "Cookie"
)

// Locale resolves the language the request renders in and injects it for the
// templates. An explicit choice wins over the browser: the lang cookie first,
// then Accept-Language, then def.
func Locale(def i18n.Locale) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addVary(w.Header(), headerCookie)
			addVary(w.Header(), headerAcceptLanguage)

			ctx := i18n.WithLocale(r.Context(), resolveLocale(r, def))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveLocale applies the cookie -> Accept-Language -> def precedence, an
// unparseable cookie degrading to negotiation rather than to nothing.
func resolveLocale(r *http.Request, def i18n.Locale) i18n.Locale {
	if cookie, err := r.Cookie(LocaleCookie); err == nil {
		if locale, ok := i18n.Parse(strings.TrimSpace(cookie.Value)); ok {
			return locale
		}
	}

	return i18n.Negotiate(r.Header.Get(headerAcceptLanguage), def)
}
