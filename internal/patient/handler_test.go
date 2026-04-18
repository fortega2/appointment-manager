package patient

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	handlerPatientsPath      = "/api/v1/patients"
	handlerContentTypeHeader = "Content-Type"
	handlerJSONContentType   = "application/json"
	handlerProblemType       = "application/problem+json"

	handlerPatientFirstName       = "Laura"
	handlerPatientLastName        = "Gomez"
	handlerPatientPhone           = "1133334444"
	handlerPatientEmail           = "laura@mail.com"
	handlerPatientInsuranceID     = 1
	handlerPatientInsuranceNumber = "12345678901"
	handlerPatientBigSize         = 1 << 20
)

func createPatientBody(firstName, lastName, phone, email string, insuranceID int, insuranceNumber string, notes *string) string {
	if notes == nil {
		return fmt.Sprintf(
			`{"first_name":%q,"last_name":%q,"phone":%q,"email":%q,"health_insurance":%d,"insurance_number":%q}`,
			firstName,
			lastName,
			phone,
			email,
			insuranceID,
			insuranceNumber,
		)
	}

	return fmt.Sprintf(
		`{"first_name":%q,"last_name":%q,"phone":%q,"email":%q,"health_insurance":%d,"insurance_number":%q,"clinical_notes":%q}`,
		firstName,
		lastName,
		phone,
		email,
		insuranceID,
		insuranceNumber,
		*notes,
	)
}

func newPatientHandlerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMuxWithPatientHandler(t *testing.T) *http.ServeMux {
	t.Helper()

	h, err := NewHandler(newPatientHandlerTestLogger(), &Repository{})
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		logger   *slog.Logger
		repo     *Repository
		expected error
	}{
		{name: "nil logger", logger: nil, repo: &Repository{}, expected: ErrNilLogger},
		{name: "nil repository", logger: newPatientHandlerTestLogger(), repo: nil, expected: ErrNilRepository},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := NewHandler(tt.logger, tt.repo)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestRegisterHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	h, err := NewHandler(newPatientHandlerTestLogger(), &Repository{})
	require.NoError(t, err)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterHandlers(mux)
	})
}

func TestCreateEndpointDecodeFailures(t *testing.T) {
	t.Parallel()

	validBody := createPatientBody(
		handlerPatientFirstName,
		handlerPatientLastName,
		handlerPatientPhone,
		handlerPatientEmail,
		handlerPatientInsuranceID,
		handlerPatientInsuranceNumber,
		nil,
	)
	bodyWithUnknownField := strings.TrimSuffix(validBody, "}") + `,"extra":"field"}`

	tests := []struct {
		name        string
		body        string
		contentType string
		expected    int
	}{
		{name: "invalid json", body: "{", contentType: handlerJSONContentType, expected: http.StatusBadRequest},
		{name: "missing content type", body: validBody, contentType: "", expected: http.StatusUnsupportedMediaType},
		{name: "unknown field", body: bodyWithUnknownField, contentType: handlerJSONContentType, expected: http.StatusBadRequest},
		{name: "multiple json values", body: validBody + validBody, contentType: handlerJSONContentType, expected: http.StatusBadRequest},
		{name: "body too large", body: createPatientBody(strings.Repeat("A", handlerPatientBigSize), handlerPatientLastName, handlerPatientPhone, handlerPatientEmail, handlerPatientInsuranceID, handlerPatientInsuranceNumber, nil), contentType: handlerJSONContentType, expected: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := newMuxWithPatientHandler(t)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, handlerPatientsPath, bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set(handlerContentTypeHeader, tt.contentType)
			}

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.expected, rec.Code)
			assert.Equal(t, handlerProblemType, rec.Header().Get(handlerContentTypeHeader))
		})
	}
}

func TestCreateEndpointDomainValidationFailure(t *testing.T) {
	t.Parallel()

	mux := newMuxWithPatientHandler(t)

	body := createPatientBody(
		"",
		handlerPatientLastName,
		handlerPatientPhone,
		handlerPatientEmail,
		handlerPatientInsuranceID,
		handlerPatientInsuranceNumber,
		nil,
	)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, handlerPatientsPath, bytes.NewBufferString(body))
	req.Header.Set(handlerContentTypeHeader, handlerJSONContentType)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, handlerProblemType, rec.Header().Get(handlerContentTypeHeader))
	assert.Contains(t, rec.Body.String(), ErrFirstNameRequired.Error())
}
