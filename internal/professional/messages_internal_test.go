package professional

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"appointment-manager/internal/i18n"
)

const fallbackKey = errKeyCreate

func TestValidationErrorKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"first name required", ErrFirstNameRequired, errKeyFirstNameRequired},
		{"last name required", ErrLastNameRequired, errKeyLastNameRequired},
		{"phone required", ErrPhoneRequired, errKeyPhoneRequired},
		{"unknown error falls back", errors.New("boom"), fallbackKey},
		{"nil error falls back", nil, fallbackKey},
		{"wrapped sentinel is still matched", fmt.Errorf("create professional: %w", ErrPhoneRequired), errKeyPhoneRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, validationErrorKey(tt.err, fallbackKey))
		})
	}
}

func TestValidationErrorKeyKeepsTheCallerFallback(t *testing.T) {
	t.Parallel()

	unmatched := errors.New("boom")

	assert.Equal(t, errKeyCreate, validationErrorKey(unmatched, errKeyCreate))
	assert.Equal(t, errKeyUpdate, validationErrorKey(unmatched, errKeyUpdate))
}

func TestValidationErrorKeysRenderInEveryLocale(t *testing.T) {
	t.Parallel()

	errs := []error{ErrFirstNameRequired, ErrLastNameRequired, ErrPhoneRequired, errors.New("boom")}

	for _, locale := range []i18n.Locale{i18n.LocaleES, i18n.LocaleEN} {
		t.Run(string(locale), func(t *testing.T) {
			t.Parallel()

			ctx := i18n.WithLocale(t.Context(), locale)
			for _, err := range errs {
				rendered := i18n.T(ctx, validationErrorKey(err, fallbackKey))

				assert.NotContains(t, rendered, "MISSING")
				assert.NotEmpty(t, rendered)
			}
		})
	}
}

func TestSpecialtyLabelKey(t *testing.T) {
	t.Parallel()

	key, ok := specialtyLabelKey(specialtyKinesiology)
	assert.True(t, ok)
	assert.Equal(t, specialtyKeyKinesiology, key)

	_, ok = specialtyLabelKey("cardiology")
	assert.False(t, ok, "an unmapped specialty must fall back to its raw value")
}
