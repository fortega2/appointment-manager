package health_test

import (
	"appointment-manager/internal/health"
	"appointment-manager/internal/web"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	healthPath            = "/healthz"
	readyPath             = "/readyz"
	contentTypeHeader     = "Content-Type"
	healthContentTypeJSON = "application/json"
	problemContentType    = "application/problem+json"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMuxWithHandler(t *testing.T, checkReady func(context.Context) error, timeout time.Duration) *http.ServeMux {
	t.Helper()

	h, err := health.NewHandler(newTestLogger(), checkReady, timeout)
	require.NoError(t, err)

	mux := http.NewServeMux()
	h.RegisterHandlers(mux)

	return mux
}

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	t.Run("nil logger", func(t *testing.T) {
		t.Parallel()

		h, err := health.NewHandler(nil, func(context.Context) error { return nil }, time.Second)

		require.Error(t, err)
		assert.Nil(t, h)
		assert.ErrorIs(t, err, health.ErrNilLogger)
	})

	t.Run("nil checker", func(t *testing.T) {
		t.Parallel()

		h, err := health.NewHandler(newTestLogger(), nil, time.Second)

		require.Error(t, err)
		assert.Nil(t, h)
		assert.ErrorIs(t, err, health.ErrNilReadinessCheck)
	})
}

func TestRegisterHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	h, err := health.NewHandler(newTestLogger(), func(context.Context) error { return nil }, time.Second)
	require.NoError(t, err)

	mux := http.NewServeMux()
	assert.NotPanics(t, func() {
		h.RegisterHandlers(mux)
	})
}

func TestLivenessEndpointReturnsStatusOK(t *testing.T) {
	t.Parallel()

	mux := newMuxWithHandler(t, func(context.Context) error { return nil }, time.Second)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, healthPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, healthContentTypeJSON, rec.Header().Get(contentTypeHeader))

	var response struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, "ok", response.Status)
}

func TestReadinessEndpointReturnsStatusOKWhenCheckerSucceeds(t *testing.T) {
	t.Parallel()

	called := false
	mux := newMuxWithHandler(t, func(ctx context.Context) error {
		called = true
		_, hasDeadline := ctx.Deadline()
		assert.True(t, hasDeadline)
		return nil
	}, time.Second)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, readyPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, healthContentTypeJSON, rec.Header().Get(contentTypeHeader))
}

func TestReadinessEndpointReturnsServiceUnavailableWhenCheckerFails(t *testing.T) {
	t.Parallel()

	mux := newMuxWithHandler(t, func(context.Context) error {
		return errors.New("db unavailable")
	}, time.Second)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, readyPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))

	var problem web.ProblemDetail
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, http.StatusServiceUnavailable, problem.Status)
	assert.Equal(t, "service not ready", problem.Detail)
	assert.Equal(t, readyPath, problem.Instance)
}

func TestReadinessEndpointReturnsServiceUnavailableWhenCheckerTimesOut(t *testing.T) {
	t.Parallel()

	mux := newMuxWithHandler(t, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, time.Millisecond)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, readyPath, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, problemContentType, rec.Header().Get(contentTypeHeader))
}
