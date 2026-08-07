package slot

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Same-package because validationErrorKey is unexported: it is the seam that
// keeps Go's wrapped error text off the screen, so it is worth pinning directly.
func TestValidationErrorKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"invalid professional", ErrInvalidProfessionalID, errKeyInvalidProfessional},
		{"invalid time range", ErrInvalidTimeRange, errKeyInvalidTimeRange},
		{"invalid max capacity", ErrInvalidMaxCapacity, errKeyInvalidMaxCapacity},
		{"invalid date", ErrInvalidDate, errKeyInvalidDate},
		{"date time inconsistency", ErrDateTimeInconsistency, errKeyDateTimeInconsistency},
		{"unknown error stays generic", errors.New("boom"), errKeyUnexpected},
		{"nil error stays generic", nil, errKeyUnexpected},
		// NewSlot wraps its sentinel, which is the whole reason the UI cannot
		// render err.Error(): the wrapping is for the logs, not the user.
		{"wrapped sentinel is still matched", fmt.Errorf("validate slot: %w", ErrInvalidTimeRange), errKeyInvalidTimeRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, validationErrorKey(tt.err))
		})
	}
}
