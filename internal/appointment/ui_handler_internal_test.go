package appointment

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
		expectedMsg    string
	}{
		{name: "invalid reference", err: ErrInvalidAppointmentReference, expectedStatus: http.StatusNotFound, expectedMsg: "Appointment not found"},
		{name: "cannot attend now", err: ErrAppointmentCannotAttendNow, expectedStatus: http.StatusUnprocessableEntity, expectedMsg: "Appointment can only be attended during slot time"},
		{name: "cannot attend status", err: ErrAppointmentCannotAttendWithStatus, expectedStatus: http.StatusConflict, expectedMsg: "Appointment cannot be attended from current status"},
		{name: "cannot cancel status", err: ErrAppointmentCannotCancelWithStatus, expectedStatus: http.StatusConflict, expectedMsg: "Appointment cannot be cancelled from current status"},
		{name: "status changed", err: ErrAppointmentStatusChanged, expectedStatus: http.StatusConflict, expectedMsg: "Appointment status changed, please refresh"},
		{name: "unmapped error", err: errors.New(boomErrMessage), expectedStatus: http.StatusInternalServerError, expectedMsg: "Failed to process request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, msg := resolveUIActionProblem(tt.err)

			assert.Equal(t, tt.expectedStatus, status)
			assert.Equal(t, tt.expectedMsg, msg)
		})
	}
}

func TestResolveUICreateProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedMsg    string
	}{
		{name: "slot required", err: ErrSlotIDRequired, expectedStatus: http.StatusBadRequest, expectedMsg: "Slot is required"},
		{name: "invalid slot", err: ErrInvalidSlotID, expectedStatus: http.StatusBadRequest, expectedMsg: "Invalid slot selected"},
		{name: "patient required", err: ErrPatientIDRequired, expectedStatus: http.StatusBadRequest, expectedMsg: "Patient is required"},
		{name: "invalid patient", err: ErrInvalidPatientID, expectedStatus: http.StatusBadRequest, expectedMsg: "Invalid patient selected"},
		{name: "professional required", err: ErrProfessionalIDRequired, expectedStatus: http.StatusBadRequest, expectedMsg: "Professional is required"},
		{name: "invalid professional", err: ErrInvalidProfessionalID, expectedStatus: http.StatusBadRequest, expectedMsg: "Invalid professional selected"},
		{name: "multiple active detected", err: ErrMultipleActiveAppointmentsDetected, expectedStatus: http.StatusConflict, expectedMsg: "Patient already has an active appointment in that time slot"},
		{name: "slot blocked", err: ErrSlotBlocked, expectedStatus: http.StatusConflict, expectedMsg: "Selected slot is blocked"},
		{name: "slot without availability", err: ErrSlotWithoutAvailability, expectedStatus: http.StatusConflict, expectedMsg: "Selected slot has no available spots"},
		{name: "no active prescription", err: ErrNoActivePrescription, expectedStatus: http.StatusConflict, expectedMsg: "Patient has no active prescription"},
		{name: "no remaining sessions", err: ErrNoRemainingSessions, expectedStatus: http.StatusConflict, expectedMsg: "Patient's prescription has no remaining sessions"},
		{name: "invalid reference", err: ErrInvalidAppointmentReference, expectedStatus: http.StatusNotFound, expectedMsg: "Referenced entity not found"},
		// AssistantID is always derived from the session in the UI create flow (never from user
		// input), so these two errors can no longer be produced there; they now fall through to
		// the default case below instead of the dedicated 400 branches the JSON API still has.
		{name: "assistant id required no longer has a dedicated branch", err: ErrAssistantIDRequired, expectedStatus: http.StatusInternalServerError, expectedMsg: "Failed to create appointment"},
		{name: "invalid assistant id no longer has a dedicated branch", err: ErrInvalidAssistantID, expectedStatus: http.StatusInternalServerError, expectedMsg: "Failed to create appointment"},
		{name: "unmapped error", err: errors.New(boomErrMessage), expectedStatus: http.StatusInternalServerError, expectedMsg: "Failed to create appointment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, msg := resolveUICreateProblem(tt.err)

			assert.Equal(t, tt.expectedStatus, status)
			assert.Equal(t, tt.expectedMsg, msg)
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
