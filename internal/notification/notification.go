// Package notification resolves who to tell about a domain event and delivers
// it. It exports a concrete *Service and no interface: consumers declare the
// function type they need and bind a method value to it.
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
	flushTimeout = 5 * time.Second
	sendTimeout  = 5 * time.Second

	tracerName = "appointment-manager/internal/notification"
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

// Metrics records what becomes of each notification. A nil value passed to the
// constructor is replaced by a no-op implementation.
type Metrics interface {
	RecordNotificationDroppedQueueFull()
	RecordNotificationDroppedUnknownKind()
	RecordNotificationSent(kind string)
	RecordNotificationNoRecipients(kind string)
	RecordNotificationLookupFailed(kind string)
	ObserveNotificationSend(ctx context.Context, kind string, duration time.Duration)
}

// Service owns the notification queue and the goroutine that drains it.
type Service struct {
	logger                  *slog.Logger
	queue                   chan Event
	resolveSlotCancellation slotCancellationFunc
	metrics                 Metrics
	tickerInterval          time.Duration
}

// NewService builds a notification service that drains its queue every
// tickerInterval. bufferSize caps how many notifications may wait at once.
func NewService(
	logger *slog.Logger,
	tickerInterval time.Duration,
	bufferSize int,
	resolveSlotCancellation slotCancellationFunc,
	notificationMetrics Metrics,
) (*Service, error) {
	errs := validate(logger, tickerInterval, bufferSize, resolveSlotCancellation)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	if notificationMetrics == nil {
		notificationMetrics = noopMetrics{}
	}

	return &Service{
		logger:                  logger,
		queue:                   make(chan Event, bufferSize),
		resolveSlotCancellation: resolveSlotCancellation,
		metrics:                 notificationMetrics,
		tickerInterval:          tickerInterval,
	}, nil
}

// QueueDepth reports how many notifications are waiting right now.
func (s *Service) QueueDepth() int { return len(s.queue) }

// QueueCapacity reports the queue's configured size.
func (s *Service) QueueCapacity() int { return cap(s.queue) }

// NotifySlotCancelled queues a notification for a cancelled slot. It never
// blocks: a saturated queue drops the event rather than stalling the producer.
func (s *Service) NotifySlotCancelled(ctx context.Context, slotID uuid.UUID) {
	s.enqueue(ctx, Event{Kind: EventSlotCancelled, SlotID: slotID})
}

// Run drains the queue on every tick until ctx is cancelled, then makes one
// final pass. It blocks, so callers run it in a goroutine.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tickerInterval)
	defer ticker.Stop()

	s.logger.InfoContext(ctx, "notification worker started", slog.Duration("interval", s.tickerInterval))

	for {
		select {
		case <-ticker.C:
			s.drain(ctx)
		case <-ctx.Done():
			s.flush(ctx)
			s.logger.InfoContext(ctx, "notification worker stopped")
			return
		}
	}
}

// SendSlotCancelled delivers a slot cancellation synchronously, so the caller
// learns whether it succeeded. It matches outbox.Handler.
func (s *Service) SendSlotCancelled(ctx context.Context, slotID uuid.UUID, _ json.RawMessage) error {
	return s.send(ctx, Event{Kind: EventSlotCancelled, SlotID: slotID})
}

func (s *Service) enqueue(ctx context.Context, event Event) {
	select {
	case s.queue <- event:
	default:
		s.metrics.RecordNotificationDroppedQueueFull()
		s.logger.WarnContext(ctx, "notification queue is full, dropping notification",
			slog.String("slot_id", event.SlotID.String()),
			slog.Int("kind", int(event.Kind)),
		)
	}
}

// flush performs the shutdown drain. ctx is already cancelled by then, so the
// work needs a context of its own or every send fails instantly.
func (s *Service) flush(ctx context.Context) {
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
	defer cancel()

	s.drain(flushCtx)
}

// drain sends every event buffered at the moment it runs and then returns: a
// drain parked on the channel would never observe shutdown.
func (s *Service) drain(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case event := <-s.queue:
			_ = s.send(ctx, event)
		default:
			return
		}
	}
}

// send delivers one event under its own timeout, span and duration observation.
// The span is a root: Event stays pointer-free, so it carries no trace context
// (ADR 0002).
func (s *Service) send(ctx context.Context, event Event) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	kind := event.Kind.String()

	ctx, span := tracer.Start(ctx, "notification.Service.send")

	var err error
	defer func() { tracing.EndSpan(span, err) }()

	span.SetAttributes(
		attribute.String("notification.kind", kind),
		attribute.String("slot_id", event.SlotID.String()),
	)

	// Timed inside the span so the observation picks up its trace_id as an exemplar.
	start := time.Now()
	err = s.dispatch(ctx, event)
	s.metrics.ObserveNotificationSend(ctx, kind, time.Since(start))

	return err
}

func (s *Service) dispatch(ctx context.Context, event Event) error {
	switch event.Kind {
	case EventSlotCancelled:
		return s.sendSlotCancelled(ctx, event.SlotID)
	default:
		s.metrics.RecordNotificationDroppedUnknownKind()
		s.logger.ErrorContext(ctx, "unknown notification kind, dropping notification",
			slog.Int("kind", int(event.Kind)),
		)

		return ErrUnknownEventKind
	}
}

// sendSlotCancelled resolves who was affected by a cancelled slot and delivers
// to each of them. Delivery is still a placeholder: a log line per recipient.
// Recipients are identified by opaque id only, never by name or contact detail.
func (s *Service) sendSlotCancelled(ctx context.Context, slotID uuid.UUID) error {
	kind := EventSlotCancelled.String()

	cancellation, err := s.resolveSlotCancellation(ctx, slotID)
	if err != nil {
		s.metrics.RecordNotificationLookupFailed(kind)
		s.logger.ErrorContext(ctx, "failed to resolve slot cancellation recipients",
			slog.String("slot_id", slotID.String()),
			slog.Any("error", err),
		)

		return err
	}

	if len(cancellation.Recipients) == 0 {
		s.metrics.RecordNotificationNoRecipients(kind)
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

	s.metrics.RecordNotificationSent(kind)
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
func validate(
	logger *slog.Logger,
	tickerInterval time.Duration,
	bufferSize int,
	resolveSlotCancellation slotCancellationFunc,
) []error {
	errs := make([]error, 0)

	if logger == nil {
		errs = append(errs, ErrNilLogger)
	}

	if tickerInterval <= 0 {
		errs = append(errs, ErrInvalidTickerInterval)
	}

	if bufferSize <= 0 {
		errs = append(errs, ErrInvalidBufferSize)
	}

	if resolveSlotCancellation == nil {
		errs = append(errs, ErrNilSlotCancellationFunc)
	}

	return errs
}

// noopMetrics is the default Metrics used when the service is built without a
// recorder.
type noopMetrics struct{}

func (noopMetrics) RecordNotificationDroppedQueueFull() {
	// RecordNotificationDroppedQueueFull is intentionally empty: no metrics recorder was configured.
}

func (noopMetrics) RecordNotificationDroppedUnknownKind() {
	// RecordNotificationDroppedUnknownKind is intentionally empty: no metrics recorder was configured.
}

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
