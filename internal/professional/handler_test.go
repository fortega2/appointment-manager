package professional

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
	handlerProfessionalsPath = "/api/v1/professionals"
	handlerContentTypeHeader = "Content-Type"
	handlerJSONContentType   = "application/json"
	handlerProblemType       = "application/problem+json"

	handlerFirstName = "Laura"
	handlerLastName  = "Gomez"
	handlerPhone     = "1133334444"
	handlerBigSize   = 1 << 20
)

func createProfessionalBody(firstName, lastName, phone string) string {
	return fmt.Sprintf(`{"first_name":%q,"last_name":%q,"phone":%q}`, firstName, lastName, phone)
}

func newHandlerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMuxWithProfessionalHandler(t *testing.T) *http.ServeMux {
	t.Helper()

	h, err := NewHandler(newHandlerTestLogger(), &Repository{})
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
		{name: "nil repository", logger: newHandlerTestLogger(), repo: nil, expected: ErrNilRepository},
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

	h, err := NewHandler(newHandlerTestLogger(), &Repository{})
	require.NoError(t, err)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterHandlers(mux)
	})
}

func TestCreateEndpointDecodeFailures(t *testing.T) {
	t.Parallel()

	validBody := createProfessionalBody(handlerFirstName, handlerLastName, handlerPhone)
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
		{name: "body too large", body: createProfessionalBody(strings.Repeat("A", handlerBigSize), handlerLastName, handlerPhone), contentType: handlerJSONContentType, expected: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := newMuxWithProfessionalHandler(t)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, handlerProfessionalsPath, bytes.NewBufferString(tt.body))
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

	mux := newMuxWithProfessionalHandler(t)

	body := createProfessionalBody("", handlerLastName, handlerPhone)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, handlerProfessionalsPath, bytes.NewBufferString(body))
	req.Header.Set(handlerContentTypeHeader, handlerJSONContentType)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, handlerProblemType, rec.Header().Get(handlerContentTypeHeader))
	assert.Contains(t, rec.Body.String(), ErrFirstNameRequired.Error())
}
