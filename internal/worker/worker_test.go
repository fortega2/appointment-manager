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
	tickerInterval = time.Millisecond
	changedCount   = int64(3)
	boomError      = "boom"
	jobName        = "expire-overdue"

	logCompletedMessage = "worker job completed"
	logFailedMessage    = "worker job failed"
	logNoChangeMessage  = "worker job changed nothing"

	eventuallyTimeout = time.Second
)

type jobMock struct {
	mock.Mock
}

func (m *jobMock) Run(ctx context.Context) (int64, error) {
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
	job := (&jobMock{}).Run

	tests := []struct {
		name     string
		logger   *slog.Logger
		jobName  string
		job      worker.JobFunc
		interval time.Duration
		wantErr  error
	}{
		{name: "nil logger", logger: nil, jobName: jobName, job: job, interval: tickerInterval, wantErr: worker.ErrNilLogger},
		{name: "empty job name", logger: logger, jobName: "", job: job, interval: tickerInterval, wantErr: worker.ErrEmptyJobName},
		{name: "blank job name", logger: logger, jobName: "   ", job: job, interval: tickerInterval, wantErr: worker.ErrEmptyJobName},
		{name: "nil job", logger: logger, jobName: jobName, job: nil, interval: tickerInterval, wantErr: worker.ErrNilJob},
		{name: "zero interval", logger: logger, jobName: jobName, job: job, interval: 0, wantErr: worker.ErrInvalidTickerInterval},
		{name: "negative interval", logger: logger, jobName: jobName, job: job, interval: -tickerInterval, wantErr: worker.ErrInvalidTickerInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, err := worker.NewWorker(tt.logger, tt.jobName, tt.job, tt.interval)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, w)
		})
	}
}

func TestNewWorkerSuccess(t *testing.T) {
	t.Parallel()

	w, err := worker.NewWorker(slog.New(slog.DiscardHandler), jobName, (&jobMock{}).Run, tickerInterval)

	require.NoError(t, err)
	assert.NotNil(t, w)
}

func TestWorkerRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		count       int64
		jobErr      error
		wantLog     string
		notWantLogs []string
	}{
		{
			name:        "reports the rows a run changed",
			count:       changedCount,
			wantLog:     logCompletedMessage,
			notWantLogs: []string{logFailedMessage},
		},
		{
			name:        "changing nothing is a quiet no-op",
			count:       0,
			wantLog:     "",
			notWantLogs: []string{logCompletedMessage, logFailedMessage, logNoChangeMessage},
		},
		{
			name:        "logs error when the job fails",
			count:       0,
			jobErr:      errors.New(boomError),
			wantLog:     logFailedMessage,
			notWantLogs: []string{logCompletedMessage},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := &syncBuffer{}
			// Info level so the count==0 Debug ("worker job changed nothing") is filtered out.
			logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

			firstCall := make(chan struct{})
			var once sync.Once
			job := &jobMock{}
			job.On("Run", mock.Anything).
				Return(tt.count, tt.jobErr).
				Run(func(mock.Arguments) {
					once.Do(func() { close(firstCall) })
				})

			w, err := worker.NewWorker(logger, jobName, job.Run, tickerInterval)
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
				t.Fatal("worker did not run the job")
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
			// Every line must carry the job name so concurrent workers stay
			// distinguishable in a shared log stream.
			if logs != "" {
				assert.Contains(t, logs, `"job":"`+jobName+`"`)
			}
		})
	}
}
