package notification_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

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

	kindSlotCancelled = "slot_cancelled"
	kindUnknown       = "unknown"

	outcomeSent         = "sent"
	outcomeNoRecipients = "no_recipients"
	outcomeLookupFailed = "lookup_failed"
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
	return oneRecipient(uuid.NewV7().String()), nil
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

// processedKey mirrors the label pair on appt_notifications_processed_total, so
// the spy records the same (kind, outcome) breakdown Prometheus would and a
// test can assert the kind reached the recorder rather than only the outcome.
type processedKey struct {
	kind    string
	outcome string
}

// metricsSpy records what the service instrumented. The drain writes to it from
// its own goroutine while the test reads, so it is mutex-guarded for the same
// reason syncBuffer is; without that, -race fails rather than the assertions.
type metricsSpy struct {
	mu               sync.Mutex
	processed        map[processedKey]int
	observed         []string
	droppedQueueFull int
	droppedUnknown   int
}

func newMetricsSpy() *metricsSpy {
	return &metricsSpy{processed: make(map[processedKey]int)}
}

func (m *metricsSpy) RecordNotificationDroppedQueueFull() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.droppedQueueFull++
}

func (m *metricsSpy) RecordNotificationDroppedUnknownKind() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.droppedUnknown++
}

func (m *metricsSpy) RecordNotificationSent(kind string) {
	m.recordProcessed(kind, outcomeSent)
}

func (m *metricsSpy) RecordNotificationNoRecipients(kind string) {
	m.recordProcessed(kind, outcomeNoRecipients)
}

func (m *metricsSpy) RecordNotificationLookupFailed(kind string) {
	m.recordProcessed(kind, outcomeLookupFailed)
}

func (m *metricsSpy) ObserveNotificationSend(_ context.Context, kind string, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.observed = append(m.observed, kind)
}

func (m *metricsSpy) recordProcessed(kind, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.processed[processedKey{kind: kind, outcome: outcome}]++
}

// snapshot copies every counter under one lock, so a reader cannot see a send
// counted in one field but not yet in another.
func (m *metricsSpy) snapshot() metricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	return metricsSnapshot{
		droppedQueueFull: m.droppedQueueFull,
		droppedUnknown:   m.droppedUnknown,
		processed:        maps.Clone(m.processed),
		observed:         slices.Clone(m.observed),
	}
}

type metricsSnapshot struct {
	processed        map[processedKey]int
	observed         []string
	droppedQueueFull int
	droppedUnknown   int
}

// totalProcessed sums every outcome, so a test can assert that one event
// produced exactly one outcome rather than naming each one.
func (s metricsSnapshot) totalProcessed() int {
	total := 0
	for _, count := range s.processed {
		total += count
	}

	return total
}

// newRecordingService builds a service whose logs land in a buffer the test can
// read. The lookup is passed as a plain func literal, which is all an
// unexported func-typed parameter needs from another package.
//
// It passes no metrics recorder on purpose: every behavioural test in this file
// therefore runs against the no-op implementation, which is what proves an
// uninstrumented service still works rather than panicking on a nil recorder.
// Tests that assert on instrumentation use newMeteredService instead.
func newRecordingService(
	t *testing.T,
	interval time.Duration,
	size int,
	lookup func(context.Context, uuid.UUID) (notification.SlotCancellation, error),
) (*notification.Service, *syncBuffer) {
	t.Helper()

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc, err := notification.NewService(logger, interval, size, lookup, nil)
	require.NoError(t, err)

	return svc, buf
}

// newMeteredService is newRecordingService with a recorder attached.
func newMeteredService(
	t *testing.T,
	interval time.Duration,
	size int,
	lookup func(context.Context, uuid.UUID) (notification.SlotCancellation, error),
) (*notification.Service, *metricsSpy) {
	t.Helper()

	spy := newMetricsSpy()
	svc, err := notification.NewService(slog.New(slog.DiscardHandler), interval, size, lookup, spy)
	require.NoError(t, err)

	return svc, spy
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

			svc, err := notification.NewService(tt.logger, tt.interval, tt.size, tt.lookup, nil)

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

	svc, err := notification.NewService(slog.New(slog.DiscardHandler), drainInterval, bufferSize, noRecipients, nil)

	require.NoError(t, err)
	assert.NotNil(t, svc)

	// A nil recorder is a valid configuration, not a validation failure: the
	// queue must run in a deployment that exports no metrics at all.
	assert.Equal(t, bufferSize, svc.QueueCapacity())
	assert.Equal(t, 0, svc.QueueDepth())
}

func TestServiceDrainsQueuedNotifications(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize, withRecipient)
	stop := runService(t, svc)

	slotIDs := []uuid.UUID{
		uuid.NewV7(),
		uuid.NewV7(),
		uuid.NewV7(),
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
			svc.NotifySlotCancelled(context.Background(), uuid.NewV7())
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

	slotID := uuid.NewV7()
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
		svc.NotifySlotCancelled(context.Background(), uuid.NewV7())
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

	slotID := uuid.NewV7()
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

	patientID := uuid.NewV7()
	lookup := func(_ context.Context, _ uuid.UUID) (notification.SlotCancellation, error) {
		return oneRecipient(patientID.String()), nil
	}

	svc, buf := newRecordingService(t, drainInterval, bufferSize, lookup)
	stop := runService(t, svc)

	slotID := uuid.NewV7()
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

	svc.NotifySlotCancelled(context.Background(), uuid.NewV7())

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

	svc.NotifySlotCancelled(context.Background(), uuid.NewV7())

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

	failing := uuid.NewV7()
	lookup := func(_ context.Context, slotID uuid.UUID) (notification.SlotCancellation, error) {
		if slotID == failing {
			return notification.SlotCancellation{}, errors.New(lookupBoomError)
		}

		return oneRecipient(uuid.NewV7().String()), nil
	}

	svc, buf := newRecordingService(t, drainInterval, bufferSize, lookup)
	stop := runService(t, svc)

	healthy := uuid.NewV7()
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

// The kind reaches Prometheus as a label value, so an unrecognised kind must
// collapse onto one series instead of opening a new one per bad value.
func TestEventKindStringIsBoundedForUnknownKinds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, kindSlotCancelled, notification.EventSlotCancelled.String())

	for _, kind := range []notification.EventKind{0, -1, 99, 32767} {
		assert.Equal(t, kindUnknown, kind.String(), "kind %d must not become its own label value", kind)
	}
}

// The counter behind "is the buffer big enough?": a saturated queue must count
// every event it turns away, not merely log the first.
func TestServiceCountsQueueFullDrops(t *testing.T) {
	t.Parallel()

	// No drain loop is started, so nothing ever empties the queue.
	svc, spy := newMeteredService(t, time.Hour, 1, noRecipients)

	const attempts = 10
	for range attempts {
		svc.NotifySlotCancelled(context.Background(), uuid.NewV7())
	}

	got := spy.snapshot()
	// One event fits the buffer; the remaining nine have nowhere to go.
	assert.Equal(t, attempts-1, got.droppedQueueFull)
	assert.Equal(t, 0, got.droppedUnknown)
	assert.Empty(t, got.observed, "a dropped event is never sent, so it must not be timed")
}

func TestServiceRecordsSendOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lookup  func(context.Context, uuid.UUID) (notification.SlotCancellation, error)
		name    string
		outcome string
	}{
		{
			name:    "delivered",
			lookup:  withRecipient,
			outcome: outcomeSent,
		},
		{
			// Nobody had booked the slot. An ordinary result, and it must stay
			// distinguishable from a delivery on a dashboard.
			name:    "nobody to notify",
			lookup:  noRecipients,
			outcome: outcomeNoRecipients,
		},
		{
			// The event is already off the queue and nothing retries it, so
			// this notification is lost rather than late.
			name:    "recipients could not be resolved",
			lookup:  failingLookup,
			outcome: outcomeLookupFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, spy := newMeteredService(t, drainInterval, bufferSize, tt.lookup)
			stop := runService(t, svc)

			svc.NotifySlotCancelled(context.Background(), uuid.NewV7())

			want := processedKey{kind: kindSlotCancelled, outcome: tt.outcome}
			require.Eventually(t, func() bool {
				return spy.snapshot().processed[want] == 1
			}, eventuallyTimeout, drainInterval)

			stop()

			// Exactly one outcome is recorded per event: a send counted twice,
			// or under two outcomes, would make the breakdown add up to more
			// than the traffic.
			got := spy.snapshot()
			assert.Equal(t, 1, got.totalProcessed())
			assert.Equal(t, 0, got.droppedQueueFull)

			// Every dequeued event is timed, including the failure: a send that
			// errored or timed out is exactly what the histogram is for.
			assert.Equal(t, []string{kindSlotCancelled}, got.observed)
		})
	}
}

func TestServiceObservesEverySendOnce(t *testing.T) {
	t.Parallel()

	svc, spy := newMeteredService(t, drainInterval, bufferSize, withRecipient)
	stop := runService(t, svc)

	const notifications = 3
	for range notifications {
		svc.NotifySlotCancelled(context.Background(), uuid.NewV7())
	}

	require.Eventually(t, func() bool {
		return len(spy.snapshot().observed) == notifications
	}, eventuallyTimeout, drainInterval)

	stop()

	got := spy.snapshot()
	assert.Equal(t, notifications, got.processed[processedKey{kind: kindSlotCancelled, outcome: outcomeSent}])
	assert.Len(t, got.observed, notifications)
}
