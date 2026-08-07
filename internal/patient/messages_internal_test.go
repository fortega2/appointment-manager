package patient

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"appointment-manager/internal/i18n"
)

// Same-package because validationErrorKey is unexported: it is the seam that
// keeps Go's wrapped error text off the screen, so it is worth pinning directly.
func TestValidationErrorKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantKey  string
		wantArgs i18n.M
	}{
		{"first name required", ErrFirstNameRequired, errKeyFirstNameRequired, nil},
		{"first name too long", ErrFirstNameTooLong, errKeyFirstNameTooLong, i18n.M{"count": maxNameLength}},
		{"last name required", ErrLastNameRequired, errKeyLastNameRequired, nil},
		{"last name too long", ErrLastNameTooLong, errKeyLastNameTooLong, i18n.M{"count": maxNameLength}},
		{"phone required", ErrPhoneRequired, errKeyPhoneRequired, nil},
		{"email required", ErrEmailRequired, errKeyEmailRequired, nil},
		{"email too long", ErrEmailTooLong, errKeyEmailTooLong, i18n.M{"count": maxEmailLength}},
		{"health insurance required", ErrHealthInsuranceRequired, errKeyHealthInsuranceRequired, nil},
		{"insurance number required", ErrInsuranceNumberRequired, errKeyInsuranceNumberRequired, nil},
		{"insurance number length", ErrInvalidInsuranceNumberLength, errKeyInvalidInsuranceNumberLength, i18n.M{"count": insuranceNumberLength}},
		{"unknown error stays generic", errors.New("boom"), errKeyUnexpected, nil},
		{"nil error stays generic", nil, errKeyUnexpected, nil},
		{"wrapped sentinel is still matched", fmt.Errorf("create patient: %w", ErrPhoneRequired), errKeyPhoneRequired, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotKey, gotArgs := validationErrorKey(tt.err)

			assert.Equal(t, tt.wantKey, gotKey)
			assert.Equal(t, tt.wantArgs, gotArgs)
		})
	}
}

// The limits are interpolated so the copy cannot drift from the rule it
// enforces; this proves the number actually reaches the rendered message.
func TestValidationErrorKeyInterpolatesTheLimit(t *testing.T) {
	t.Parallel()

	key, args := validationErrorKey(ErrInvalidInsuranceNumberLength)

	tests := []struct {
		locale i18n.Locale
		want   string
	}{
		{i18n.LocaleES, fmt.Sprintf("El número de afiliado tiene que tener exactamente %d caracteres", insuranceNumberLength)},
		{i18n.LocaleEN, fmt.Sprintf("Insurance number must be exactly %d characters", insuranceNumberLength)},
	}

	for _, tt := range tests {
		t.Run(string(tt.locale), func(t *testing.T) {
			t.Parallel()

			ctx := i18n.WithLocale(t.Context(), tt.locale)

			assert.Equal(t, tt.want, i18n.T(ctx, key, args))
		})
	}
}
