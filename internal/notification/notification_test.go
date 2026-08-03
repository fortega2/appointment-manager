package notification_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/notification"
)

const (
	drainInterval = time.Millisecond
	bufferSize    = 8

	sentMessage           = "slot cancellation notification sent"
	droppedMessage        = "notification queue is full, dropping notification"
	stoppedMessage        = "notification worker stopped"
	noRecipientsMessage   = "cancelled slot had no recipients to notify"
	lookupFailedMessage   = "failed to resolve slot cancellation recipients"
	recipientDebugMessage = "slot cancellation recipient"

	eventuallyTimeout = time.Second
	settleDelay       = 50 * time.Millisecond

	lookupBoomError = "boom"
)

// Contact details that must never reach the log backend, so the assertions can
// prove they did not.
const (
	recipientName  = "Bruno Ferreyra"
	recipientEmail = "bruno.ferreyra@example.com"
	recipientPhone = "+54 11 5555 0100"
)

func oneRecipient(id string) notification.SlotCancellation {
	email := recipientEmail

	return notification.SlotCancellation{
		StartTime:            time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC),
		EndTime:              time.Date(2026, 6, 5, 10, 30, 0, 0, time.UTC),
		ProfessionalFullName: "Dr. Ruiz",
		Recipients: []notification.Recipient{
			{ID: id, FullName: recipientName, Email: &email, Phone: recipientPhone},
		},
	}
}

// noRecipients is the lookup used by tests that do not care who was affected:
// an empty result is a valid outcome, so it stands in as the quiet default.
func noRecipients(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
	return notification.SlotCancellation{}, nil
}

// withRecipient resolves every slot to a single affected patient.
func withRecipient(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
	return oneRecipient(uuid.Must(uuid.NewV7()).String()), nil
}

// failingLookup stands in for an unreachable database.
func failingLookup(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
	return notification.SlotCancellation{}, errors.New(lookupBoomError)
}

// countMessages counts records whose msg is exactly this one. Plain substring
// matching would be ambiguous here: "failed to resolve slot cancellation
// recipients" contains "slot cancellation recipient", so asserting one message
// is absent would silently pass on the other.
func countMessages(logs, msg string) int {
	return strings.Count(logs, `"msg":"`+msg+`"`)
}

func hasMessage(logs, msg string) bool {
	return countMessages(logs, msg) > 0
}

// syncBuffer is a goroutine-safe buffer so the drain goroutine can write logs
// while the test reads them without triggering the race detector.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// newRecordingService builds a service whose logs land in a buffer the test can
// read. The lookup is passed as a plain func literal, which is all an
// unexported func-typed parameter needs from another package.
func newRecordingService(
	t *testing.T,
	interval time.Duration,
	size int,
	lookup func(context.Context, uuid.UUID) (notification.SlotCancellation, error),
) (*notification.Service, *syncBuffer) {
	t.Helper()

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc, err := notification.NewService(logger, interval, size, lookup)
	require.NoError(t, err)

	return svc, buf
}

// runService starts the drain loop and returns a stop func that cancels it and
// waits for the goroutine to exit, so no test leaks one into the next.
func runService(t *testing.T, svc *notification.Service) func() {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Run(ctx)
	}()

	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(eventuallyTimeout):
			t.Fatal("notification worker did not stop after context cancellation")
		}
	}
}

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name     string
		logger   *slog.Logger
		lookup   func(context.Context, uuid.UUID) (notification.SlotCancellation, error)
		interval time.Duration
		size     int
		wantErrs []error
	}{
		{
			name:     "nil logger",
			lookup:   noRecipients,
			interval: drainInterval,
			size:     bufferSize,
			wantErrs: []error{notification.ErrNilLogger},
		},
		{
			name:     "zero interval",
			logger:   logger,
			lookup:   noRecipients,
			interval: 0,
			size:     bufferSize,
			wantErrs: []error{notification.ErrInvalidTickerInterval},
		},
		{
			name:     "negative interval",
			logger:   logger,
			lookup:   noRecipients,
			interval: -drainInterval,
			size:     bufferSize,
			wantErrs: []error{notification.ErrInvalidTickerInterval},
		},
		{
			name:     "zero buffer size",
			logger:   logger,
			lookup:   noRecipients,
			interval: drainInterval,
			size:     0,
			wantErrs: []error{notification.ErrInvalidBufferSize},
		},
		{
			name:     "negative buffer size",
			logger:   logger,
			lookup:   noRecipients,
			interval: drainInterval,
			size:     -1,
			wantErrs: []error{notification.ErrInvalidBufferSize},
		},
		{
			name:     "nil recipient lookup",
			logger:   logger,
			interval: drainInterval,
			size:     bufferSize,
			wantErrs: []error{notification.ErrNilSlotCancellationFunc},
		},
		{
			// Every problem is reported at once so a misconfigured deployment
			// does not surface them one restart at a time.
			name:     "every problem at once",
			interval: 0,
			size:     0,
			wantErrs: []error{
				notification.ErrNilLogger,
				notification.ErrInvalidTickerInterval,
				notification.ErrInvalidBufferSize,
				notification.ErrNilSlotCancellationFunc,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := notification.NewService(tt.logger, tt.interval, tt.size, tt.lookup)

			require.Error(t, err)
			assert.Nil(t, svc)
			for _, wantErr := range tt.wantErrs {
				assert.ErrorIs(t, err, wantErr)
			}
		})
	}
}

func TestNewServiceSuccess(t *testing.T) {
	t.Parallel()

	svc, err := notification.NewService(slog.New(slog.DiscardHandler), drainInterval, bufferSize, noRecipients)

	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestServiceDrainsQueuedNotifications(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, withRecipient)
	stop := runService(t, svc)

	slotIDs := []uuid.UUID{
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
	}
	for _, slotID := range slotIDs {
		svc.NotifySlotCancelled(context.Background(), slotID)
	}

	require.Eventually(t, func() bool {
		return countMessages(buf.String(), sentMessage) == len(slotIDs)
	}, eventuallyTimeout, drainInterval, "every queued notification should be sent")

	stop()

	// Each notification must carry its own slot, not merely add up to the
	// right count.
	logs := buf.String()
	for _, slotID := range slotIDs {
		assert.Contains(t, logs, slotID.String())
	}
	assert.False(t, hasMessage(logs, droppedMessage))
}

// A saturated queue must cost the caller nothing: the notification is dropped
// and logged rather than blocking the request goroutine that raised it.
func TestNotifySlotCancelledDropsWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	// No drain loop is started, so nothing ever empties the queue.
	svc, buf := newRecordingService(t, time.Hour, 1, noRecipients)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10 {
			svc.NotifySlotCancelled(context.Background(), uuid.Must(uuid.NewV7()))
		}
	}()

	select {
	case <-done:
	case <-time.After(eventuallyTimeout):
		t.Fatal("NotifySlotCancelled blocked on a full queue")
	}

	logs := buf.String()
	assert.True(t, hasMessage(logs, droppedMessage))
	// One event fits the buffer; the remaining nine have nowhere to go.
	assert.Equal(t, 9, countMessages(logs, droppedMessage))
}

// Notifications raised just before shutdown are still delivered: the drain loop
// makes a final pass on a context of its own, since the one it was given is
// already cancelled by then.
func TestServiceFlushesQueueOnShutdown(t *testing.T) {
	t.Parallel()

	// An interval far longer than the test guarantees no ordinary tick fires,
	// so anything delivered here came from the shutdown flush.
	svc, buf := newRecordingService(t, time.Hour, bufferSize, withRecipient)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Run(ctx)
	}()

	slotID := uuid.Must(uuid.NewV7())
	svc.NotifySlotCancelled(context.Background(), slotID)

	cancel()

	select {
	case <-done:
	case <-time.After(eventuallyTimeout):
		t.Fatal("notification worker did not stop after context cancellation")
	}

	logs := buf.String()
	assert.True(t, hasMessage(logs, sentMessage), "a notification queued before shutdown must still be sent")
	assert.Contains(t, logs, slotID.String())
	assert.True(t, hasMessage(logs, stoppedMessage))
}

func TestServiceRunStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, noRecipients)
	stop := runService(t, svc)

	stop()

	assert.True(t, hasMessage(buf.String(), stoppedMessage))

	// Producers outlive the drain loop, so enqueuing after it stopped must stay
	// safe -- the channel is never closed.
	assert.NotPanics(t, func() {
		svc.NotifySlotCancelled(context.Background(), uuid.Must(uuid.NewV7()))
	})
}

// A cancelled caller context must not suppress the notification: the event
// outlives the request that raised it.
func TestNotifySlotCancelledIgnoresCallerContextCancellation(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, noRecipients)
	stop := runService(t, svc)
	defer stop()

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	slotID := uuid.Must(uuid.NewV7())
	svc.NotifySlotCancelled(cancelledCtx, slotID)

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), slotID.String())
	}, eventuallyTimeout, drainInterval, "the notification should survive its caller's context")
}

func TestServiceDrainIsQuietWhenNothingIsQueued(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, noRecipients)
	stop := runService(t, svc)

	// Let several ticks pass with an empty queue.
	time.Sleep(settleDelay)
	stop()

	assert.False(t, hasMessage(buf.String(), sentMessage))
	assert.False(t, hasMessage(buf.String(), droppedMessage))
}

// Contact details are needed to deliver a notification but must never be
// written to the log backend, which has a different audience and retention from
// the database. Recipients are identified by opaque id only.
func TestSendSlotCancelledKeepsContactDetailsOutOfLogs(t *testing.T) {
	t.Parallel()

	patientID := uuid.Must(uuid.NewV7())
	lookup := func(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
		return oneRecipient(patientID.String()), nil
	}

	svc, buf := newRecordingService(t, drainInterval, bufferSize, lookup)
	stop := runService(t, svc)

	slotID := uuid.Must(uuid.NewV7())
	svc.NotifySlotCancelled(context.Background(), slotID)

	require.Eventually(t, func() bool {
		return hasMessage(buf.String(), sentMessage)
	}, eventuallyTimeout, drainInterval)

	stop()

	logs := buf.String()

	// The recipient is identified, and the delivery is recorded.
	assert.True(t, hasMessage(logs, recipientDebugMessage))
	assert.Contains(t, logs, patientID.String())
	assert.Contains(t, logs, `"recipients":1`)

	// None of the personal data is.
	assert.NotContains(t, logs, recipientName)
	assert.NotContains(t, logs, recipientEmail)
	assert.NotContains(t, logs, recipientPhone)
}

// A slot nobody booked is an ordinary outcome, not a failure: it is recorded
// and nothing is delivered.
func TestSendSlotCancelledWithoutRecipientsIsNotAnError(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, noRecipients)
	stop := runService(t, svc)

	svc.NotifySlotCancelled(context.Background(), uuid.Must(uuid.NewV7()))

	require.Eventually(t, func() bool {
		return hasMessage(buf.String(), noRecipientsMessage)
	}, eventuallyTimeout, drainInterval)

	stop()

	logs := buf.String()
	assert.False(t, hasMessage(logs, lookupFailedMessage), "an empty result is not a failure")
	assert.False(t, hasMessage(logs, sentMessage), "nothing was delivered")
	assert.False(t, hasMessage(logs, recipientDebugMessage))
}

// A failed lookup is logged and abandoned: nothing is delivered, and the drain
// loop carries on with the rest of the queue.
func TestSendSlotCancelledStopsWhenLookupFails(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, failingLookup)
	stop := runService(t, svc)

	svc.NotifySlotCancelled(context.Background(), uuid.Must(uuid.NewV7()))

	require.Eventually(t, func() bool {
		return hasMessage(buf.String(), lookupFailedMessage)
	}, eventuallyTimeout, drainInterval)

	stop()

	logs := buf.String()
	assert.Contains(t, logs, lookupBoomError)
	assert.False(t, hasMessage(logs, sentMessage), "a failed lookup must not report a delivery")
	assert.False(t, hasMessage(logs, recipientDebugMessage))
}

// One failing event must not cost the events queued behind it.
func TestServiceKeepsDrainingAfterALookupFailure(t *testing.T) {
	t.Parallel()

	failing := uuid.Must(uuid.NewV7())
	lookup := func(_ context.Context, slotID uuid.UUID) (notification.SlotCancellation, error) {
		if slotID == failing {
			return notification.SlotCancellation{}, errors.New(lookupBoomError)
		}

		return oneRecipient(uuid.Must(uuid.NewV7()).String()), nil
	}

	svc, buf := newRecordingService(t, drainInterval, bufferSize, lookup)
	stop := runService(t, svc)

	healthy := uuid.Must(uuid.NewV7())
	svc.NotifySlotCancelled(context.Background(), failing)
	svc.NotifySlotCancelled(context.Background(), healthy)

	require.Eventually(t, func() bool {
		logs := buf.String()
		return hasMessage(logs, lookupFailedMessage) && hasMessage(logs, sentMessage)
	}, eventuallyTimeout, drainInterval)

	stop()

	logs := buf.String()
	assert.Contains(t, logs, failing.String())
	assert.Contains(t, logs, healthy.String())
}
