package notification_test

import (
	"bytes"
	"context"
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

	sentMessage    = "slot cancellation notification"
	droppedMessage = "notification queue is full, dropping notification"
	stoppedMessage = "notification worker stopped"

	eventuallyTimeout = time.Second
	settleDelay       = 50 * time.Millisecond
)

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

func newRecordingService(t *testing.T, interval time.Duration, size int) (*notification.Service, *syncBuffer) {
	t.Helper()

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc, err := notification.NewService(logger, interval, size)
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
		interval time.Duration
		size     int
		wantErrs []error
	}{
		{
			name:     "nil logger",
			interval: drainInterval,
			size:     bufferSize,
			wantErrs: []error{notification.ErrNilLogger},
		},
		{
			name:     "zero interval",
			logger:   logger,
			interval: 0,
			size:     bufferSize,
			wantErrs: []error{notification.ErrInvalidTickerInterval},
		},
		{
			name:     "negative interval",
			logger:   logger,
			interval: -drainInterval,
			size:     bufferSize,
			wantErrs: []error{notification.ErrInvalidTickerInterval},
		},
		{
			name:     "zero buffer size",
			logger:   logger,
			interval: drainInterval,
			size:     0,
			wantErrs: []error{notification.ErrInvalidBufferSize},
		},
		{
			name:     "negative buffer size",
			logger:   logger,
			interval: drainInterval,
			size:     -1,
			wantErrs: []error{notification.ErrInvalidBufferSize},
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
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := notification.NewService(tt.logger, tt.interval, tt.size)

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

	svc, err := notification.NewService(slog.New(slog.DiscardHandler), drainInterval, bufferSize)

	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestServiceDrainsQueuedNotifications(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize)
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
		return strings.Count(buf.String(), sentMessage) == len(slotIDs)
	}, eventuallyTimeout, drainInterval, "every queued notification should be sent")

	stop()

	// Each notification must carry its own slot, not merely add up to the
	// right count.
	logs := buf.String()
	for _, slotID := range slotIDs {
		assert.Contains(t, logs, slotID.String())
	}
	assert.NotContains(t, logs, droppedMessage)
}

// A saturated queue must cost the caller nothing: the notification is dropped
// and logged rather than blocking the request goroutine that raised it.
func TestNotifySlotCancelledDropsWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	// No drain loop is started, so nothing ever empties the queue.
	svc, buf := newRecordingService(t, time.Hour, 1)

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
	assert.Contains(t, logs, droppedMessage)
	// One event fits the buffer; the remaining nine have nowhere to go.
	assert.Equal(t, 9, strings.Count(logs, droppedMessage))
}

// Notifications raised just before shutdown are still delivered: the drain loop
// makes a final pass on a context of its own, since the one it was given is
// already cancelled by then.
func TestServiceFlushesQueueOnShutdown(t *testing.T) {
	t.Parallel()

	// An interval far longer than the test guarantees no ordinary tick fires,
	// so anything delivered here came from the shutdown flush.
	svc, buf := newRecordingService(t, time.Hour, bufferSize)

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
	assert.Contains(t, logs, sentMessage, "a notification queued before shutdown must still be sent")
	assert.Contains(t, logs, slotID.String())
	assert.Contains(t, logs, stoppedMessage)
}

func TestServiceRunStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	svc, buf := newRecordingService(t, drainInterval, bufferSize)
	stop := runService(t, svc)

	stop()

	assert.Contains(t, buf.String(), stoppedMessage)

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

	svc, buf := newRecordingService(t, drainInterval, bufferSize)
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

	svc, buf := newRecordingService(t, drainInterval, bufferSize)
	stop := runService(t, svc)

	// Let several ticks pass with an empty queue.
	time.Sleep(settleDelay)
	stop()

	assert.NotContains(t, buf.String(), sentMessage)
	assert.NotContains(t, buf.String(), droppedMessage)
}
