package web

import (
	"encoding/json"
	"net/http"
)

const (
	problemJSONContentType = "application/problem+json"

	ProblemTypeInvalidJSON          = "/problems/invalid-json"
	ProblemTypeUnsupportedMediaType = "/problems/unsupported-media-type"
	ProblemTypeRequestBodyTooLarge  = "/problems/request-body-too-large"
	ProblemTypeValidationFailed     = "/problems/validation-failed"
	ProblemTypeResourceNotFound     = "/problems/resource-not-found"
	ProblemTypeConflict             = "/problems/conflict"
	ProblemTypeInternalServerError  = "/problems/internal-server-error"
)

type ProblemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

func NewProblem(status int, problemType, detail, instance string) ProblemDetail {
	return ProblemDetail{
		Type:     problemType,
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Instance: instance,
	}
}

func NewInternalServerProblem(detail, instance string) ProblemDetail {
	return NewProblem(http.StatusInternalServerError, ProblemTypeInternalServerError, detail, instance)
}

func WriteProblem(w http.ResponseWriter, problem ProblemDetail) {
	if problem.Status <= 0 {
		problem.Status = http.StatusInternalServerError
	}
	if problem.Title == "" {
		problem.Title = http.StatusText(problem.Status)
	}

	body, err := json.Marshal(problem)
	if err != nil {
		fallback := []byte(`{"type":"/problems/internal-server-error","title":"Internal Server Error","status":500,"detail":"failed to marshal problem response"}`)
		w.Header().Set("Content-Type", problemJSONContentType)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(fallback)
		return
	}

	w.Header().Set("Content-Type", problemJSONContentType)
	w.WriteHeader(problem.Status)
	_, _ = w.Write(body)
}
