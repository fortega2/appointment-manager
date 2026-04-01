package middleware

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-Id"

func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
			if requestID == "" {
				requestID = uuid.NewString()
				r.Header.Set(requestIDHeader, requestID)
			}

			w.Header().Set(requestIDHeader, requestID)
			next.ServeHTTP(w, r)
		})
	}
}
