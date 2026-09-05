// Package notification resolves who to tell about a domain event and delivers
// it. It exports a concrete *Service and no interface.
package notification

import (
	"appointment-manager/internal/tracing"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	sendTimeout = 5 * time.Second

	tracerName = "appointment-manager/internal/notification"

	kindSlotCancelled = "slot_cancelled"
)

var tracer = otel.Tracer(tracerName)

// Recipient is one person to tell about a cancelled slot.
type Recipient struct {
	FullName string
	Phone    string
	ID       string
	Email    *string
}

type SlotCancellation struct {
	StartTime            time.Time
	EndTime              time.Time
	Recipients           []Recipient
	ProfessionalFullName string
}

// slotCancellationFunc resolves who to tell about a cancelled slot. No
// recipients is an ordinary result, not an error.
type slotCancellationFunc func(ctx context.Context, slotID uuid.UUID) (SlotCancellation, error)

// Metrics records what becomes of each notification. A nil value is replaced
// by a no-op implementation.
type Metrics interface {
	RecordNotificationSent(kind string)
	RecordNotificationNoRecipients(kind string)
	RecordNotificationLookupFailed(kind string)
	ObserveNotificationSend(ctx context.Context, kind string, duration time.Duration)
}

// Service resolves and delivers notifications.
type Service struct {
	logger                  *slog.Logger
	resolveSlotCancellation slotCancellationFunc
	metrics                 Metrics
}

// NewService builds a notification service.
func NewService(
	logger *slog.Logger,
	resolveSlotCancellation slotCancellationFunc,
	notificationMetrics Metrics,
) (*Service, error) {
	errs := validate(logger, resolveSlotCancellation)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	if notificationMetrics == nil {
		notificationMetrics = noopMetrics{}
	}

	return &Service{
		logger:                  logger,
		resolveSlotCancellation: resolveSlotCancellation,
		metrics:                 notificationMetrics,
	}, nil
}

// SendSlotCancelled delivers a slot cancellation. It matches outbox.Handler.
func (s *Service) SendSlotCancelled(ctx context.Context, slotID uuid.UUID, _ json.RawMessage) error {
	return s.send(ctx, kindSlotCancelled, slotID)
}

// send delivers one event under its own timeout, span and duration observation.
// The span is a root: the producer passes no trace context (ADR 0002).
func (s *Service) send(ctx context.Context, kind string, slotID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	ctx, span := tracer.Start(ctx, "notification.Service.send")

	var err error
	defer func() { tracing.EndSpan(span, err) }()

	span.SetAttributes(
		attribute.String("notification.kind", kind),
		attribute.String("slot_id", slotID.String()),
	)

	// Timed inside the span so the observation gets its trace_id as an exemplar.
	start := time.Now()
	err = s.sendSlotCancelled(ctx, slotID)
	s.metrics.ObserveNotificationSend(ctx, kind, time.Since(start))

	return err
}

// sendSlotCancelled delivers to everyone affected by a cancelled slot. Delivery
// is still a placeholder, and recipients are logged by opaque id only.
func (s *Service) sendSlotCancelled(ctx context.Context, slotID uuid.UUID) error {
	cancellation, err := s.resolveSlotCancellation(ctx, slotID)
	if err != nil {
		s.metrics.RecordNotificationLookupFailed(kindSlotCancelled)
		s.logger.ErrorContext(ctx, "failed to resolve slot cancellation recipients",
			slog.String("slot_id", slotID.String()),
			slog.Any("error", err),
		)

		return err
	}

	if len(cancellation.Recipients) == 0 {
		s.metrics.RecordNotificationNoRecipients(kindSlotCancelled)
		s.logger.InfoContext(ctx, "cancelled slot had no recipients to notify",
			slog.String("slot_id", slotID.String()),
		)

		return nil
	}

	for _, recipient := range cancellation.Recipients {
		s.logger.DebugContext(ctx, "slot cancellation recipient",
			slog.String("slot_id", slotID.String()),
			slog.String("patient_id", recipient.ID),
			slog.Bool("has_email", recipient.Email != nil),
		)
	}

	s.metrics.RecordNotificationSent(kindSlotCancelled)
	s.logger.InfoContext(ctx, "slot cancellation notification sent",
		slog.String("slot_id", slotID.String()),
		slog.String("professional", cancellation.ProfessionalFullName),
		slog.Int("recipients", len(cancellation.Recipients)),
		slog.Time("slot_start", cancellation.StartTime),
		slog.Time("slot_end", cancellation.EndTime),
	)

	return nil
}

// validate reports every problem with the constructor arguments at once.
func validate(logger *slog.Logger, resolveSlotCancellation slotCancellationFunc) []error {
	errs := make([]error, 0)

	if logger == nil {
		errs = append(errs, ErrNilLogger)
	}

	if resolveSlotCancellation == nil {
		errs = append(errs, ErrNilSlotCancellationFunc)
	}

	return errs
}

// noopMetrics is used when the service is built without a recorder.
type noopMetrics struct{}

func (noopMetrics) RecordNotificationSent(string) {
	// RecordNotificationSent is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) RecordNotificationNoRecipients(string) {
	// RecordNotificationNoRecipients is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) RecordNotificationLookupFailed(string) {
	// RecordNotificationLookupFailed is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) ObserveNotificationSend(context.Context, string, time.Duration) {
	// ObserveNotificationSend is intentionally empty: no metrics recorder was configured.
}
