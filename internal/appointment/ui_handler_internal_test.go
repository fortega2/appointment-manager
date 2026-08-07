package appointment

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"appointment-manager/internal/i18n"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const boomErrMessage = "boom"

func TestResolveUIActionProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedKey    string
	}{
		{name: "invalid reference", err: ErrInvalidAppointmentReference, expectedStatus: http.StatusNotFound, expectedKey: errKeyNotFound},
		{name: "cannot attend now", err: ErrAppointmentCannotAttendNow, expectedStatus: http.StatusUnprocessableEntity, expectedKey: errKeyCannotAttendNow},
		{name: "cannot attend status", err: ErrAppointmentCannotAttendWithStatus, expectedStatus: http.StatusConflict, expectedKey: errKeyCannotAttendStatus},
		{name: "cannot cancel status", err: ErrAppointmentCannotCancelWithStatus, expectedStatus: http.StatusConflict, expectedKey: errKeyCannotCancelStatus},
		{name: "status changed", err: ErrAppointmentStatusChanged, expectedStatus: http.StatusConflict, expectedKey: errKeyStatusChanged},
		{name: "unmapped error", err: errors.New(boomErrMessage), expectedStatus: http.StatusInternalServerError, expectedKey: errKeyProcess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, key := resolveUIActionProblem(tt.err)

			assert.Equal(t, tt.expectedStatus, status)
			assert.Equal(t, tt.expectedKey, key)
		})
	}
}

func TestResolveUICreateProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedKey    string
	}{
		{name: "slot required", err: ErrSlotIDRequired, expectedStatus: http.StatusBadRequest, expectedKey: errKeySlotRequired},
		{name: "invalid slot", err: ErrInvalidSlotID, expectedStatus: http.StatusBadRequest, expectedKey: errKeyInvalidSlot},
		{name: "patient required", err: ErrPatientIDRequired, expectedStatus: http.StatusBadRequest, expectedKey: errKeyPatientRequired},
		{name: "invalid patient", err: ErrInvalidPatientID, expectedStatus: http.StatusBadRequest, expectedKey: errKeyInvalidPatient},
		{name: "professional required", err: ErrProfessionalIDRequired, expectedStatus: http.StatusBadRequest, expectedKey: errKeyProfessionalRequired},
		{name: "invalid professional", err: ErrInvalidProfessionalID, expectedStatus: http.StatusBadRequest, expectedKey: errKeyInvalidProfessional},
		{name: "multiple active detected", err: ErrMultipleActiveAppointmentsDetected, expectedStatus: http.StatusConflict, expectedKey: errKeyAlreadyActive},
		{name: "slot blocked", err: ErrSlotBlocked, expectedStatus: http.StatusConflict, expectedKey: errKeySlotBlocked},
		{name: "slot without availability", err: ErrSlotWithoutAvailability, expectedStatus: http.StatusConflict, expectedKey: errKeySlotNoAvailability},
		{name: "no active prescription", err: ErrNoActivePrescription, expectedStatus: http.StatusConflict, expectedKey: errKeyNoPrescription},
		{name: "no remaining sessions", err: ErrNoRemainingSessions, expectedStatus: http.StatusConflict, expectedKey: errKeyNoRemainingSessions},
		{name: "invalid reference", err: ErrInvalidAppointmentReference, expectedStatus: http.StatusNotFound, expectedKey: errKeyReferenceNotFound},
		// AssistantID is always derived from the session in the UI create flow (never from user
		// input), so these two errors can no longer be produced there; they now fall through to
		// the default case below instead of the dedicated 400 branches the JSON API still has.
		{name: "assistant id required no longer has a dedicated branch", err: ErrAssistantIDRequired, expectedStatus: http.StatusInternalServerError, expectedKey: errKeyCreate},
		{name: "invalid assistant id no longer has a dedicated branch", err: ErrInvalidAssistantID, expectedStatus: http.StatusInternalServerError, expectedKey: errKeyCreate},
		{name: "unmapped error", err: errors.New(boomErrMessage), expectedStatus: http.StatusInternalServerError, expectedKey: errKeyCreate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, key := resolveUICreateProblem(tt.err)

			assert.Equal(t, tt.expectedStatus, status)
			assert.Equal(t, tt.expectedKey, key)
		})
	}
}

func TestUIHandlerParseForm(t *testing.T) {
	t.Parallel()

	h := &UIHandler{}

	t.Run("valid form", func(t *testing.T) {
		t.Parallel()

		body := "slot_id=slot-1&patient_id=patient-1&professional_id=prof-1&notes=  hello  "
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/appointments", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		parsed, err := h.parseForm(req, rec)

		require.NoError(t, err)
		assert.Equal(t, "slot-1", parsed.SlotID)
		assert.Equal(t, "patient-1", parsed.PatientID)
		assert.Equal(t, "prof-1", parsed.ProfessionalID)
		require.NotNil(t, parsed.Notes)
		assert.Equal(t, "hello", *parsed.Notes)
	})

	t.Run("malformed body returns error", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/appointments", bytes.NewBufferString("slot_id=%zz"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		parsed, err := h.parseForm(req, rec)

		require.Error(t, err)
		assert.Nil(t, parsed)
	})
}

// The resolvers return keys, so a key that is missing from a catalog would
// reach the user as ctxi18n's marker rather than as copy. This walks every
// branch of both resolvers and proves each one renders in both languages.
func TestResolvedProblemKeysRenderInEveryLocale(t *testing.T) {
	t.Parallel()

	errs := []error{
		ErrInvalidAppointmentReference, ErrAppointmentCannotAttendNow,
		ErrAppointmentCannotAttendWithStatus, ErrAppointmentCannotCancelWithStatus,
		ErrAppointmentStatusChanged, ErrSlotIDRequired, ErrInvalidSlotID,
		ErrPatientIDRequired, ErrInvalidPatientID, ErrProfessionalIDRequired,
		ErrInvalidProfessionalID, ErrMultipleActiveAppointmentsDetected,
		ErrSlotBlocked, ErrSlotWithoutAvailability, ErrNoActivePrescription,
		ErrNoRemainingSessions, errors.New(boomErrMessage),
	}

	for _, locale := range []i18n.Locale{i18n.LocaleES, i18n.LocaleEN} {
		t.Run(string(locale), func(t *testing.T) {
			t.Parallel()

			ctx := i18n.WithLocale(t.Context(), locale)
			for _, err := range errs {
				for _, key := range []string{
					mustKey(resolveUIActionProblem(err)),
					mustKey(resolveUICreateProblem(err)),
				} {
					rendered := i18n.T(ctx, key)
					assert.NotContains(t, rendered, "MISSING", "key %q has no copy", key)
					assert.NotEmpty(t, rendered)
				}
			}
		})
	}
}

// mustKey drops the status a resolver returns alongside its key.
func mustKey(_ int, key string) string { return key }
