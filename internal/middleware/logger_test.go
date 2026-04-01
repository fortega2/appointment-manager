package middleware_test

import (
	"appointment-manager/internal/middleware"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	requestURL                = "/api/v1/assistants/123"
	routePath                 = "/api/v1/assistants/{id}"
	testRequestID             = "req-123"
	requestIDField            = "request_id"
	responseBytesField        = "response_bytes"
	requestContentLengthField = "request_content_length"
)

func TestRequestLoggerLogsRequestWithLevelByStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		expectedLevel string
	}{
		{name: "2xx logs at info", status: http.StatusCreated, expectedLevel: "INFO"},
		{name: "4xx logs at warn", status: http.StatusNotFound, expectedLevel: "WARN"},
		{name: "5xx logs at error", status: http.StatusInternalServerError, expectedLevel: "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, logs := newBufferedLogger()
			mux := http.NewServeMux()
			mux.HandleFunc("GET "+routePath, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("ok"))
			})

			handler := middleware.RequestLogger(logger)(mux)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, requestURL, nil)
			req.RemoteAddr = "127.0.0.1:34567"
			req.Header.Set("User-Agent", "middleware-test")
			req.Header.Set(requestIDHeader, testRequestID)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			entry := decodeLastLogEntry(t, logs)
			assert.Equal(t, tt.expectedLevel, entry["level"])
			assert.Equal(t, "http request completed", entry["msg"])
			assert.Equal(t, http.MethodGet, entry["method"])
			assert.Equal(t, routePath, entry["route"])
			assert.Equal(t, requestURL, entry["path"])
			assert.EqualValues(t, tt.status, entry["status"])
			assert.EqualValues(t, 2, entry[responseBytesField])
			assert.EqualValues(t, 0, entry[requestContentLengthField])
			assert.Equal(t, "127.0.0.1", entry["client_ip"])
			assert.Equal(t, "middleware-test", entry["user_agent"])
			assert.Equal(t, testRequestID, entry[requestIDField])
		})
	}
}

func TestRequestLoggerOmitsRequestIDWhenHeaderIsMissing(t *testing.T) {
	t.Parallel()

	logger, logs := newBufferedLogger()
	handler := middleware.RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	req.RemoteAddr = "malformed-addr"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	entry := decodeLastLogEntry(t, logs)
	assert.Equal(t, "/health", entry["route"])
	assert.Equal(t, "malformed-addr", entry["client_ip"])
	_, ok := entry[requestIDField]
	assert.False(t, ok)
}

func TestRequestLoggerHandlesNilLoggerAndNilHandler(t *testing.T) {
	t.Parallel()

	handler := middleware.RequestLogger(nil)(nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/unmatched", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRequestLoggerUsesFirstStatusCodeWritten(t *testing.T) {
	t.Parallel()

	logger, logs := newBufferedLogger()
	handler := middleware.RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	entry := decodeLastLogEntry(t, logs)
	assert.EqualValues(t, http.StatusInternalServerError, entry["status"])
	assert.Equal(t, "ERROR", entry["level"])
}

func newBufferedLogger() (*slog.Logger, *bytes.Buffer) {
	var logs bytes.Buffer
	handler := slog.NewJSONHandler(&logs, nil)

	return slog.New(handler), &logs
}

func decodeLastLogEntry(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()

	trimmedLogs := strings.TrimSpace(logs.String())
	require.NotEmpty(t, trimmedLogs)

	lines := strings.Split(trimmedLogs, "\n")
	require.NotEmpty(t, lines)

	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))

	return entry
}
