package web

import (
	"encoding/json"
	"net/http"
)

const problemJSONContentType = "application/problem+json"

type ProblemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
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
