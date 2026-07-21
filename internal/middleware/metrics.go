package middleware

import (
	"net/http"
	"time"
)

// HTTPMetrics records RED (rate, errors, duration) signals for HTTP requests.
// It is implemented by *metrics.Metrics; defining the interface here keeps the
// Prometheus client out of the middleware package.
type HTTPMetrics interface {
	IncInFlight()
	DecInFlight()
	ObserveRequest(method, route, statusClass string, duration time.Duration)
}

const unmatchedRoute = "unmatched"

// Metrics returns middleware that records request count, duration and the
// in-flight gauge for every request, labelling by the low-cardinality route
// template. A nil recorder makes the middleware a pass-through.
func Metrics(rec HTTPMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		if rec == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.IncInFlight()
			defer rec.DecInFlight()

			start := time.Now()
			rw := newResponseRecorder(w)

			next.ServeHTTP(rw, r)

			rec.ObserveRequest(r.Method, metricRoute(r), statusClass(rw.status), time.Since(start))
		})
	}
}

// metricRoute returns the matched route template as a bounded label value,
// collapsing unmatched requests to a constant so raw paths never explode
// cardinality (unlike the logger, which keeps the raw path for diagnostics).
func metricRoute(r *http.Request) string {
	if r.Pattern == "" {
		return unmatchedRoute
	}

	return requestRoute(r)
}

// statusClass buckets an HTTP status code into its class ("1xx".."5xx") to keep
// the request counter's cardinality bounded.
func statusClass(status int) string {
	switch {
	case status >= http.StatusInternalServerError:
		return "5xx"
	case status >= http.StatusBadRequest:
		return "4xx"
	case status >= http.StatusMultipleChoices:
		return "3xx"
	case status >= http.StatusOK:
		return "2xx"
	default:
		return "1xx"
	}
}
