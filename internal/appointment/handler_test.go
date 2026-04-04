package appointment

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appointmentsPath   = "/api/v1/appointments"
	problemContentType = "application/problem+json"
	headerContentType  = "Content-Type"
)

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		logger   *slog.Logger
		expected error
	}{
		{name: "nil logger", logger: nil, expected: ErrNilLogger},
		{name: "nil db", logger: newTestLogger(), expected: ErrNilDB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, err := NewHandler(tt.logger, nil)

			require.Error(t, err)
			assert.Nil(t, h)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestRegisterHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: newTestLogger()}
	mux := http.NewServeMux()

	assert.NotPanics(t, func() {
		h.RegisterHandlers(mux)
	})
}

func TestListEndpointInvalidQueryReturnsBadRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{name: "invalid page", query: "?page=0"},
		{name: "invalid limit", query: "?limit=0"},
		{name: "invalid status", query: "?status=9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Handler{logger: newTestLogger()}
			mux := http.NewServeMux()
			h.RegisterHandlers(mux)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, appointmentsPath+tt.query, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, problemContentType, rec.Header().Get(headerContentType))
		})
	}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
