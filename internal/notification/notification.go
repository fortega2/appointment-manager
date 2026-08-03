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
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	// flushTimeout bounds the final drain at shutdown. Delivery is best-effort,
	// so a backlog that cannot clear in time is dropped rather than holding the
	// process open.
	flushTimeout = 5 * time.Second

	// sendTimeout bounds one notification, so a single slow delivery cannot
	// stall the events queued behind it.
	sendTimeout = 5 * time.Second
)

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

// Service owns the notification queue and the goroutine that drains it.
type Service struct {
	logger                  *slog.Logger
	queue                   chan Event
	resolveSlotCancellation slotCancellationFunc
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
) (*Service, error) {
	errs := validate(logger, tickerInterval, bufferSize, resolveSlotCancellation)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return &Service{
		logger:                  logger,
		queue:                   make(chan Event, bufferSize),
		resolveSlotCancellation: resolveSlotCancellation,
		tickerInterval:          tickerInterval,
	}, nil
}

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
// Unlike worker.Worker this does not drain once before the first tick: the
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

func (s *Service) send(ctx context.Context, event Event) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	switch event.Kind {
	case EventSlotCancelled:
		s.sendSlotCancelled(ctx, event.SlotID)
	default:
		s.logger.ErrorContext(ctx, "unknown notification kind, dropping notification",
			slog.Int("kind", int(event.Kind)),
		)
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
func (s *Service) sendSlotCancelled(ctx context.Context, slotID uuid.UUID) {
	cancellation, err := s.resolveSlotCancellation(ctx, slotID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to resolve slot cancellation recipients",
			slog.String("slot_id", slotID.String()),
			slog.Any("error", err),
		)
		return
	}

	if len(cancellation.Recipients) == 0 {
		s.logger.InfoContext(ctx, "cancelled slot had no recipients to notify",
			slog.String("slot_id", slotID.String()),
		)
		return
	}

	for _, recipient := range cancellation.Recipients {
		s.logger.DebugContext(ctx, "slot cancellation recipient",
			slog.String("slot_id", slotID.String()),
			slog.String("patient_id", recipient.ID),
			slog.Bool("has_email", recipient.Email != nil),
		)
	}

	s.logger.InfoContext(ctx, "slot cancellation notification sent",
		slog.String("slot_id", slotID.String()),
		slog.String("professional", cancellation.ProfessionalFullName),
		slog.Int("recipients", len(cancellation.Recipients)),
		slog.Time("slot_start", cancellation.StartTime),
		slog.Time("slot_end", cancellation.EndTime),
	)
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
