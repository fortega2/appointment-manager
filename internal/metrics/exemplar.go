package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// traceIDLabel is the exemplar label Grafana follows to jump from a metric
// sample to the matching trace in Tempo.
const traceIDLabel = "trace_id"

// observeWithExemplar records value on obs, attaching the active trace_id as an
// OpenMetrics exemplar when a sampled span is present so a latency spike links
// straight to the trace that caused it. Without a sampled span it degrades to a
// plain observation.
func observeWithExemplar(ctx context.Context, obs prometheus.Observer, value float64) {
	if eo, ok := obs.(prometheus.ExemplarObserver); ok {
		if sc := trace.SpanContextFromContext(ctx); sc.IsSampled() {
			eo.ObserveWithExemplar(value, prometheus.Labels{traceIDLabel: sc.TraceID().String()})

			return
		}
	}

	obs.Observe(value)
}
