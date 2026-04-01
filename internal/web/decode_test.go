package web_test

import (
	"appointment-manager/internal/web"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	jSONContentType          = "application/json"
	contentTypeHeader        = "Content-Type"
	decodePath               = "/api/v1/resource"
	unknownFieldDetailPrefix = "request body contains unknown field"
	oneMegabyte              = int64(1 << 20)
	problemTypeInvalidJSON   = "/problems/invalid-json"
)

type decodeRequest struct {
	Name string `json:"name"`
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"name":"jane"}`))
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		var payload decodeRequest
		problem := web.DecodeJSON(rec, req, oneMegabyte, &payload)

		require.Nil(t, problem)
		assert.Equal(t, "jane", payload.Name)
	})

	t.Run("unsupported media type", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"name":"jane"}`))
		req.Header.Set(contentTypeHeader, "text/plain")
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, oneMegabyte, &decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusUnsupportedMediaType, problem.Status)
		assert.Equal(t, "/problems/unsupported-media-type", problem.Type)
	})

	t.Run("empty body", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, http.NoBody)
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, oneMegabyte, &decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusBadRequest, problem.Status)
		assert.Equal(t, problemTypeInvalidJSON, problem.Type)
		assert.Equal(t, "request body must not be empty", problem.Detail)
	})

	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"unknown":"value"}`))
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, oneMegabyte, &decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusBadRequest, problem.Status)
		assert.Equal(t, problemTypeInvalidJSON, problem.Type)
		assert.True(t, strings.HasPrefix(problem.Detail, unknownFieldDetailPrefix))
	})

	t.Run("invalid field type", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"name":123}`))
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, oneMegabyte, &decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusBadRequest, problem.Status)
		assert.Equal(t, problemTypeInvalidJSON, problem.Type)
		assert.Equal(t, "request body contains an invalid value for field \"name\"", problem.Detail)
	})

	t.Run("body too large", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"name":"12345"}`))
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, 8, &decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusRequestEntityTooLarge, problem.Status)
		assert.Equal(t, "/problems/request-body-too-large", problem.Type)
	})

	t.Run("multiple JSON values", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"name":"jane"}{"name":"john"}`))
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, oneMegabyte, &decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusBadRequest, problem.Status)
		assert.Equal(t, "request body must contain a single JSON object", problem.Detail)
	})

	t.Run("invalid decode target", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, decodePath, bytes.NewBufferString(`{"name":"jane"}`))
		req.Header.Set(contentTypeHeader, jSONContentType)
		rec := httptest.NewRecorder()

		problem := web.DecodeJSON(rec, req, oneMegabyte, decodeRequest{})

		require.NotNil(t, problem)
		assert.Equal(t, http.StatusInternalServerError, problem.Status)
		assert.Equal(t, "decode target must be a non-nil pointer", problem.Detail)
	})
}
