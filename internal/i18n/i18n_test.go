package i18n_test

import (
	"fmt"
	"os"
	"testing"

	"appointment-manager/internal/i18n"

	"github.com/invopop/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	welcomeKey  = "home.welcome"
	welcomeES   = "Bienvenido a Appointment Manager"
	welcomeEN   = "Welcome to Appointment Manager"
	unknownCode = "fr"
)

// TestMain loads the catalogs once, because ctxi18n keeps them in package-level
// state that parallel subtests would otherwise race on.
func TestMain(m *testing.M) {
	if err := i18n.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "loading catalogs: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestNegotiate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		acceptLanguage string
		def            i18n.Locale
		want           i18n.Locale
	}{
		{"empty header falls back", "", i18n.LocaleES, i18n.LocaleES},
		{"empty header honours def", "", i18n.LocaleEN, i18n.LocaleEN},
		{"spanish region falls back to base language", "es-AR", i18n.LocaleEN, i18n.LocaleES},
		{"english region falls back to base language", "en-US", i18n.LocaleES, i18n.LocaleEN},
		{"quality values are ranked, not ordered", "en;q=0.1,es;q=0.9", i18n.LocaleEN, i18n.LocaleES},
		{"browser default ordering", "es-AR,es;q=0.9,en;q=0.8", i18n.LocaleEN, i18n.LocaleES},
		{"unsupported language falls back", unknownCode, i18n.LocaleEN, i18n.LocaleEN},
		{"malformed header falls back", "!!!", i18n.LocaleES, i18n.LocaleES},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, i18n.Negotiate(tt.acceptLanguage, tt.def))
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		want  i18n.Locale
		valid bool
	}{
		{"spanish", "es", i18n.LocaleES, true},
		{"english", "en", i18n.LocaleEN, true},
		{"region is not an exact code", "es-AR", "", false},
		{"unsupported", unknownCode, "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := i18n.Parse(tt.raw)
			assert.Equal(t, tt.valid, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFromContext(t *testing.T) {
	t.Parallel()

	t.Run("round trips the locale", func(t *testing.T) {
		t.Parallel()

		ctx := i18n.WithLocale(t.Context(), i18n.LocaleEN)

		assert.Equal(t, i18n.LocaleEN, i18n.FromContext(ctx))
	})

	t.Run("unset context reports the fallback", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, i18n.Fallback, i18n.FromContext(t.Context()))
	})

	t.Run("unknown locale degrades to the fallback", func(t *testing.T) {
		t.Parallel()

		ctx := i18n.WithLocale(t.Context(), i18n.Locale(unknownCode))

		assert.Equal(t, i18n.Fallback, i18n.FromContext(ctx))
	})
}

func TestT(t *testing.T) {
	t.Parallel()

	t.Run("translates per locale", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, welcomeES, i18n.T(i18n.WithLocale(t.Context(), i18n.LocaleES), welcomeKey))
		assert.Equal(t, welcomeEN, i18n.T(i18n.WithLocale(t.Context(), i18n.LocaleEN), welcomeKey))
	})

	t.Run("renders real copy without middleware", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, welcomeES, i18n.T(t.Context(), welcomeKey))
	})

	t.Run("interpolates named values", func(t *testing.T) {
		t.Parallel()

		ctx := i18n.WithLocale(t.Context(), i18n.LocaleEN)

		assert.Equal(t, "Insurance number must be exactly 8 characters",
			i18n.T(ctx, "patient.error.insurance_number_length", i18n.M{"count": 8}))
	})
}

func TestN(t *testing.T) {
	t.Parallel()

	const pluralKey = "layout.nav.log_out"

	tests := []struct {
		name   string
		locale i18n.Locale
		count  int
	}{
		{"english", i18n.LocaleEN, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := i18n.WithLocale(t.Context(), tt.locale)

			assert.NotEmpty(t, i18n.N(ctx, pluralKey, tt.count))
		})
	}
}

// TestCatalogParity is the real guard against untranslated copy reaching the
// clinic: Load merges Spanish into every other locale, so a key missing from
// en.yml would silently render in Spanish rather than fail.
func TestCatalogParity(t *testing.T) {
	t.Parallel()

	es := flattenCatalog(t, "locales/es.yml", string(i18n.LocaleES))
	en := flattenCatalog(t, "locales/en.yml", string(i18n.LocaleEN))

	// Guards the comparison below against passing vacuously: if the YAML ever
	// decoded into something flatten walks past, both sets would come back empty
	// and every parity check would succeed.
	require.Contains(t, es, welcomeKey)
	require.Contains(t, en, welcomeKey)

	assert.Empty(t, missingKeys(es, en), "keys present in es.yml but missing from en.yml")
	assert.Empty(t, missingKeys(en, es), "keys present in en.yml but missing from es.yml")
}

func flattenCatalog(t *testing.T, path, code string) map[string]struct{} {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var doc map[string]map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	entries, ok := doc[code]
	require.Truef(t, ok, "%s has no top-level %q key", path, code)

	keys := make(map[string]struct{})
	flatten("", entries, keys)

	return keys
}

func flatten(prefix string, node map[string]any, keys map[string]struct{}) {
	for key, value := range node {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			flatten(full, nested, keys)

			continue
		}

		keys[full] = struct{}{}
	}
}

func missingKeys(from, in map[string]struct{}) []string {
	var missing []string
	for key := range from {
		if _, ok := in[key]; !ok {
			missing = append(missing, key)
		}
	}

	return missing
}
