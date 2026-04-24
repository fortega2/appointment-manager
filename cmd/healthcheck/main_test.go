package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunReturnsSuccessWhenTargetIsReady(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	var stderr bytes.Buffer
	code := run([]string{"-url", server.URL, "-timeout", "2s"}, &stderr)

	assert.Equal(t, healthcheckExitSuccess, code)
	assert.Empty(t, stderr.String())
}

func TestRunReturnsFailureWhenStatusIsNotOK(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	var stderr bytes.Buffer
	code := run([]string{"-url", server.URL}, &stderr)

	assert.Equal(t, healthcheckExitFailure, code)
	assert.Contains(t, stderr.String(), "unexpected status code")
}

func TestRunReturnsFailureWhenRequestTimesOut(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(30 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	var stderr bytes.Buffer
	code := run([]string{"-url", server.URL, "-timeout", "1ms"}, &stderr)

	assert.Equal(t, healthcheckExitFailure, code)
	assert.Contains(t, stderr.String(), context.DeadlineExceeded.Error())
}

func TestRunReturnsBadUsageWhenTimeoutIsInvalid(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	code := run([]string{"-timeout=0s"}, &stderr)

	assert.Equal(t, healthcheckExitBadUsage, code)
	assert.Contains(t, stderr.String(), "timeout must be greater than zero")
}

func TestCheckReadyReturnsErrorForInvalidURL(t *testing.T) {
	t.Parallel()

	err := checkReady("://invalid", time.Second)

	require.Error(t, err)
}
