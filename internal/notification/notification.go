// Package notification queues notifications raised by request handlers and
// delivers them from a background goroutine, so producing a notification never
// costs a request more than a channel send.
//
// The package exports a concrete *Service and no Notifier interface: consumers
// declare the function type they need (see slot.sendNotificationFunc) and bind
// a method value to it, which keeps the abstraction with the consumer and lets
// the transport change without any consumer knowing.
package notification

import (
	"appointment-manager/internal/tracing"
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// flushTimeout bounds the final drain at shutdown. Delivery is best-effort,
	// so a backlog that cannot clear in time is dropped rather than holding the
	// process open.
	flushTimeout = 5 * time.Second

	// sendTimeout bounds one notification, so a single slow delivery cannot
	// stall the events queued behind it.
	sendTimeout = 5 * time.Second

	tracerName = "appointment-manager/internal/notification"
)

// tracer is resolved once; the global delegate forwards to the real provider
// installed at start-up.
var tracer = otel.Tracer(tracerName)

// Recipient is one person to tell about a cancelled slot.
type Recipient struct {
	FullName string
	Phone    string
	ID       string
	Email    *string
}

// SlotCancellation is everything needed to tell people a slot is off. The slot
// and professional details are held once rather than repeated on every
// Recipient, because a single cancellation is what the whole group shares.
type SlotCancellation struct {
	StartTime            time.Time
	EndTime              time.Time
	Recipients           []Recipient
	ProfessionalFullName string
}

// slotCancellationFunc resolves who to tell about a cancelled slot, and what to
// tell them.
// No recipients is an ordinary result, not an error: cancelling a slot nobody
// booked is normal, and the caller checks the length.
type slotCancellationFunc func(ctx context.Context, slotID uuid.UUID) (SlotCancellation, error)

// Metrics records what becomes of each notification. It is satisfied by
// *metrics.Metrics; a nil value passed to the constructor is replaced by a
// no-op implementation so metrics stay an optional dependency.
//
// The drop reasons get a method each rather than a reason argument: the queue
// knows it ran out of room, it does not know what a metric label is called.
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
// tickerInterval. bufferSize caps how many notifications may wait at once;
// beyond that, new ones are dropped rather than blocking their producer.
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

// QueueDepth reports how many notifications are waiting right now, and
// QueueCapacity the ceiling they are waiting against. They are read on demand
// rather than published on every enqueue, so an observer can sample them as
// often or as rarely as it likes without the queue paying for it.
func (s *Service) QueueDepth() int { return len(s.queue) }

// QueueCapacity reports the queue's configured size.
func (s *Service) QueueCapacity() int { return cap(s.queue) }

// NotifySlotCancelled queues a notification for a cancelled slot. It never
// blocks: if the queue is saturated the event is dropped and logged, so a
// stalled or slow transport can never stall an HTTP handler. Its signature
// matches slot.sendNotificationFunc so it binds there as a method value.
func (s *Service) NotifySlotCancelled(ctx context.Context, slotID uuid.UUID) {
	s.enqueue(ctx, Event{Kind: EventSlotCancelled, SlotID: slotID})
}

// Run drains the queue on every tick until ctx is cancelled, then makes one
// final pass so notifications raised just before shutdown are not lost. It
// blocks, so callers run it in a goroutine and stop it by cancelling ctx.
//
// Unlike worker.Group this does not drain once before the first tick: the
// queue is necessarily empty at start-up, so that pass would always be a no-op.
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

// flush performs the shutdown drain. ctx is already cancelled by the time this
// runs, so the work is given a context of its own -- derived with
// WithoutCancel -- or every send would fail the instant it started.
func (s *Service) flush(ctx context.Context) {
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
	defer cancel()

	s.drain(flushCtx)
}

// drain sends every event buffered at the moment it runs and then returns. It
// deliberately does not wait for more work: a drain parked on the channel would
// never observe shutdown. The ctx check bounds the pass so a producer flooding
// the queue cannot keep it running past a cancellation.
func (s *Service) drain(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case event := <-s.queue:
			s.send(ctx, event)
		default:
			return
		}
	}
}

// send delivers one event under its own timeout, span and duration observation.
//
// The span is a root: it is deliberately not linked to the request that queued
// the event. Carrying the producer's trace context would mean putting a
// SpanContext on Event, and that type holds a TraceState backed by a slice --
// the queue's buffer is one contiguous, unscanned allocation precisely because
// Event has no pointers in it (see ADR 0002). A parentless span costs a link
// between two traces; the alternative costs that property on every event.
func (s *Service) send(ctx context.Context, event Event) {
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

	// Timed inside the span so the observation picks up its trace_id as an
	// exemplar: observeWithExemplar reads the SpanContext off ctx, which is what
	// links a slow send on a Grafana panel to the trace that produced it.
	start := time.Now()
	err = s.dispatch(ctx, event)
	s.metrics.ObserveNotificationSend(ctx, kind, time.Since(start))
}

// dispatch routes one event to the sender for its kind. It returns the error
// that should mark the span, which is not the same as everything worth
// logging: resolving nobody is an ordinary outcome and returns nil.
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
// to each of them. The delivery itself is still a placeholder -- a log line per
// recipient rather than an email -- and is the only part of the pipeline that
// is: replacing this function's inner loop is what makes delivery real.
//
// Nothing here logs a name, address or phone number. These records reach the
// log backend, which has a different audience and retention from the database,
// so recipients are identified by opaque id only.
//
// It returns an error only for a genuine failure. A cancellation nobody had
// booked resolves to no recipients, which is an ordinary result and returns
// nil, so it does not mark the span as failed or feed error-rate alerting.
func (s *Service) sendSlotCancelled(ctx context.Context, slotID uuid.UUID) error {
	kind := EventSlotCancelled.String()

	cancellation, err := s.resolveSlotCancellation(ctx, slotID)
	if err != nil {
		// The event has already left the queue and nothing retries it, so this
		// notification is not late -- it is lost. The counter is what makes
		// that visible beyond this log line.
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

// validate reports every problem with the constructor arguments at once, rather
// than the first, so a misconfigured deployment surfaces all of them in one go.
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
// recorder, so instrumentation is optional and tests need not wire it.
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
