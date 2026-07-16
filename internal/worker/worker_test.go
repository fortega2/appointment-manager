package worker_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/worker"
)

const (
	testTickerInterval = time.Millisecond
	expiredCount       = int64(3)
	boomError          = "boom"

	logExpiredMessage = "expired overdue appointments"
	logFailedMessage  = "failed to expire overdue appointments"
	logNoOverdueMsg   = "no overdue appointments"

	eventuallyTimeout = time.Second
)

type expirerMock struct {
	mock.Mock
}

func (m *expirerMock) ExpireOverdue(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// syncBuffer is a goroutine-safe buffer so the worker goroutine can write logs
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

func TestNewWorkerValidation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	expirer := (&expirerMock{}).ExpireOverdue

	tests := []struct {
		name     string
		logger   *slog.Logger
		expirer  worker.ExpireOverdueFunc
		interval time.Duration
		wantErr  error
	}{
		{name: "nil logger", logger: nil, expirer: expirer, interval: testTickerInterval, wantErr: worker.ErrNilLogger},
		{name: "nil expirer", logger: logger, expirer: nil, interval: testTickerInterval, wantErr: worker.ErrNilAppointmentExpirer},
		{name: "zero interval", logger: logger, expirer: expirer, interval: 0, wantErr: worker.ErrInvalidTickerInterval},
		{name: "negative interval", logger: logger, expirer: expirer, interval: -testTickerInterval, wantErr: worker.ErrInvalidTickerInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, err := worker.NewWorker(tt.logger, tt.expirer, tt.interval)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, w)
		})
	}
}

func TestNewWorkerSuccess(t *testing.T) {
	t.Parallel()

	w, err := worker.NewWorker(slog.New(slog.DiscardHandler), (&expirerMock{}).ExpireOverdue, testTickerInterval)

	require.NoError(t, err)
	assert.NotNil(t, w)
}

func TestWorkerRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		count       int64
		expireErr   error
		wantLog     string
		notWantLogs []string
	}{
		{
			name:        "expires overdue appointments",
			count:       expiredCount,
			wantLog:     logExpiredMessage,
			notWantLogs: []string{logFailedMessage},
		},
		{
			name:        "no overdue appointments is a quiet no-op",
			count:       0,
			wantLog:     "",
			notWantLogs: []string{logExpiredMessage, logFailedMessage, logNoOverdueMsg},
		},
		{
			name:        "logs error when expiration fails",
			count:       0,
			expireErr:   errors.New(boomError),
			wantLog:     logFailedMessage,
			notWantLogs: []string{logExpiredMessage},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := &syncBuffer{}
			// Info level so the count==0 Debug ("no overdue appointments") is filtered out.
			logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

			firstCall := make(chan struct{})
			var once sync.Once
			expirer := &expirerMock{}
			expirer.On("ExpireOverdue", mock.Anything).
				Return(tt.count, tt.expireErr).
				Run(func(mock.Arguments) {
					once.Do(func() { close(firstCall) })
				})

			w, err := worker.NewWorker(logger, expirer.ExpireOverdue, testTickerInterval)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				w.Run(ctx)
				close(done)
			}()

			// Wait until the worker has ticked at least once.
			select {
			case <-firstCall:
			case <-time.After(eventuallyTimeout):
				t.Fatal("worker did not call ExpireOverdue")
			}

			cancel()

			select {
			case <-done:
			case <-time.After(eventuallyTimeout):
				t.Fatal("worker did not stop after context cancellation")
			}

			logs := buf.String()
			if tt.wantLog != "" {
				assert.Contains(t, logs, tt.wantLog)
			}
			for _, unwanted := range tt.notWantLogs {
				assert.NotContains(t, logs, unwanted)
			}
		})
	}
}
