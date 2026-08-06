// Package i18n renders user-facing copy in the visitor's language.
//
// It wraps github.com/invopop/ctxi18n, which owns the message catalogs, so no
// template or handler imports that library directly: it is pre-1.0 and the copy
// call sites are spread across every UI package, so the swap cost is confined to
// this file.
package i18n

import (
	"context"
	"embed"
	"fmt"
	"sync"

	"github.com/invopop/ctxi18n"
	catalog "github.com/invopop/ctxi18n/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.yml
var localesFS embed.FS

// Locale is a language the UI can be rendered in.
type Locale string

const (
	// LocaleES is Spanish.
	LocaleES Locale = "es"
	// LocaleEN is English.
	LocaleEN Locale = "en"
)

// CookieName is the cookie carrying an explicit language choice, written by the
// language switcher and read back by the locale middleware.
const CookieName = "lang"

// Fallback is the locale used when nothing else resolves: no catalog loaded, an
// unknown code, or a request that never passed through the locale middleware.
// It is deliberately a constant rather than the configurable DEFAULT_LOCALE, so
// that a rendering path which bypasses the middleware still produces real copy.
const Fallback = LocaleES

// M holds named interpolation values for T and N, substituted into %{name}
// placeholders in the catalog.
type M = catalog.M

// matcher negotiates an Accept-Language header against the supported locales.
// x/text is used rather than ctxi18n.Match because the latter compares codes
// verbatim: it ignores q-values (so "en;q=0.1,es;q=0.9" would pick English) and
// never falls back from a region to its base language (so a browser sending
// only "es-AR" would match nothing).
var matcher = language.NewMatcher([]language.Tag{
	language.Spanish,
	language.English,
})

var loadCatalogs = sync.OnceValue(func() error {
	return ctxi18n.LoadWithDefault(localesFS, catalog.Code(LocaleES))
})

// Load reads the embedded catalogs, and is idempotent. Call it at startup to
// surface a broken catalog there; every lookup loads on demand anyway, so a
// rendering path that skipped it still produces copy rather than a marker.
func Load() error {
	if err := loadCatalogs(); err != nil {
		return fmt.Errorf("loading locale catalogs: %w", err)
	}

	return nil
}

// Parse converts a raw code into a supported Locale, reporting whether it is
// one. It accepts only exact codes, so it suits validating a cookie or an
// environment variable; use Negotiate for an Accept-Language header.
func Parse(raw string) (Locale, bool) {
	switch Locale(raw) {
	case LocaleES:
		return LocaleES, true
	case LocaleEN:
		return LocaleEN, true
	default:
		return "", false
	}
}

// Negotiate picks the best supported locale for an Accept-Language header,
// returning def when the header is empty, malformed, or names no supported
// language.
func Negotiate(acceptLanguage string, def Locale) Locale {
	if acceptLanguage == "" {
		return def
	}

	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil {
		return def
	}

	_, index, confidence := matcher.Match(tags...)
	if confidence == language.No {
		return def
	}

	switch index {
	case 0:
		return LocaleES
	case 1:
		return LocaleEN
	default:
		return def
	}
}

// WithLocale records the locale the request should render in. It never fails:
// an unknown locale, or a catalog that failed to load, leaves the context
// untouched so lookups degrade to Fallback.
func WithLocale(ctx context.Context, locale Locale) context.Context {
	// Every path that resolves a locale reaches this, so loading here is what
	// keeps a caller that skipped Load from rendering ctxi18n's missing-locale
	// marker into the page.
	_ = Load()

	loaded := ctxi18n.Get(catalog.Code(locale))
	if loaded == nil {
		loaded = ctxi18n.Get(catalog.Code(Fallback))
	}
	if loaded == nil {
		return ctx
	}

	return loaded.WithContext(ctx)
}

// FromContext reports the locale the request renders in, defaulting to Fallback
// when the middleware never ran.
func FromContext(ctx context.Context) Locale {
	if loaded := catalog.GetLocale(ctx); loaded != nil {
		return Locale(loaded.Code())
	}

	return Fallback
}

// T translates key for the context's locale. Pass an M to fill %{name}
// placeholders.
func T(ctx context.Context, key string, args ...any) string {
	return catalog.T(withFallbackLocale(ctx), key, args...)
}

// N translates key for the context's locale, choosing the plural form that
// matches n. The catalog entry holds "one" and "other" sub-keys, and n is
// interpolated through an M like any other value.
func N(ctx context.Context, key string, n int, args ...any) string {
	return catalog.N(withFallbackLocale(ctx), key, n, args...)
}

// withFallbackLocale guarantees a locale is present, because ctxi18n renders a
// literal "missing locale" marker into the page otherwise.
func withFallbackLocale(ctx context.Context) context.Context {
	if catalog.GetLocale(ctx) != nil {
		return ctx
	}

	return WithLocale(ctx, Fallback)
}
