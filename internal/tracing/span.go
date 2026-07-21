package tracing

import (
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// EndSpan closes span, recording err as the span's error and setting the Error
// status when err is non-nil. Centralising this keeps the error-to-span
// translation identical across the service spans and the database query tracer.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	span.End()
}
