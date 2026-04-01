package web_test

import (
	"appointment-manager/internal/web"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	problemContentType = "Content-Type"
	problemJSONType    = "application/problem+json"
)

func TestWriteProblem(t *testing.T) {
	t.Parallel()

	t.Run("writes RFC problem details", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		problem := web.ProblemDetail{
			Type:     "/problems/invalid-json",
			Title:    "Bad Request",
			Status:   http.StatusBadRequest,
			Detail:   "request body must not be empty",
			Instance: "/api/v1/assistants",
		}

		web.WriteProblem(rec, problem)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, problemJSONType, rec.Header().Get(problemContentType))

		var decoded web.ProblemDetail
		err := json.Unmarshal(rec.Body.Bytes(), &decoded)
		require.NoError(t, err)
		assert.Equal(t, problem, decoded)
	})

	t.Run("defaults status and title", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()

		web.WriteProblem(rec, web.ProblemDetail{Type: "/problems/internal-server-error"})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Equal(t, problemJSONType, rec.Header().Get(problemContentType))

		var decoded web.ProblemDetail
		err := json.Unmarshal(rec.Body.Bytes(), &decoded)
		require.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, decoded.Status)
		assert.Equal(t, http.StatusText(http.StatusInternalServerError), decoded.Title)
	})
}
