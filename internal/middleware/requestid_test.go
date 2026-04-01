package middleware_test

import (
	"appointment-manager/internal/middleware"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	requestIDHeader   = "X-Request-Id"
	existingRequestID = "req-from-client-123"
)

func TestRequestIDPreservesIncomingHeader(t *testing.T) {
	t.Parallel()

	var capturedHeader string
	handler := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get(requestIDHeader)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil)
	req.Header.Set(requestIDHeader, existingRequestID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, existingRequestID, capturedHeader)
	assert.Equal(t, existingRequestID, rec.Header().Get(requestIDHeader))
}

func TestRequestIDGeneratesHeaderWhenMissing(t *testing.T) {
	t.Parallel()

	var capturedHeader string
	handler := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get(requestIDHeader)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.NotEmpty(t, capturedHeader)
	_, err := uuid.Parse(capturedHeader)
	require.NoError(t, err)
	assert.Equal(t, capturedHeader, rec.Header().Get(requestIDHeader))
}

func TestRequestIDHandlesNilHandler(t *testing.T) {
	t.Parallel()

	handler := middleware.RequestID()(nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/unmatched", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	require.NotEmpty(t, rec.Header().Get(requestIDHeader))
}
