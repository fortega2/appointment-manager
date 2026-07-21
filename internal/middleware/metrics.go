package middleware

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// HTTPMetrics records RED (rate, errors, duration) signals for HTTP requests.
// It is implemented by *metrics.Metrics; defining the interface here keeps the
// Prometheus client out of the middleware package. The request context is passed
// so the recorder can attach a trace exemplar to the duration observation.
type HTTPMetrics interface {
	IncInFlight()
	DecInFlight()
	ObserveRequest(ctx context.Context, method, route, statusClass string, duration time.Duration)
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

			route := metricRoute(r)
			renameActiveSpan(r.Context(), r.Method, route)
			rec.ObserveRequest(r.Context(), r.Method, route, statusClass(rw.status), time.Since(start))
		})
	}
}

// renameActiveSpan renames the active OTel server span to "{method} {route}"
// once routing has resolved the low-cardinality route template. The span (if
// any) was created before routing ran and so only knew the HTTP method; this
// reuses the same route template the request counter is labelled with instead
// of computing it again. A no-op when no span is recording (tracing disabled,
// or the request never reached an OTel-wrapped handler).
func renameActiveSpan(ctx context.Context, method, route string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.SetName(method + " " + route)
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
