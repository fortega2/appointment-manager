package worker_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
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
	jobTimeout     = time.Second
	changedCount   = int64(3)
	boomError      = "boom"
	jobName        = "expire-overdue"
	otherJobName   = "expire-sessions"

	logCompletedMessage = "worker job completed"
	logFailedMessage    = "worker job failed"
	logNoChangeMessage  = "worker job changed nothing"
	logStartedMessage   = "worker started"
	logStoppedMessage   = "worker stopped"

	eventuallyTimeout = time.Second
)

func validConfig() worker.Config {
	return worker.Config{Interval: tickerInterval, JobTimeout: jobTimeout}
}

type jobMock struct {
	mock.Mock
}

func (m *jobMock) Run(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// syncBuffer is a goroutine-safe buffer so the worker goroutines can write logs
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

// signalJob returns a job that closes the returned channel on its first run.
func signalJob(count int64, jobErr error) (*jobMock, <-chan struct{}) {
	firstCall := make(chan struct{})
	var once sync.Once

	job := &jobMock{}
	job.On("Run", mock.Anything).
		Return(count, jobErr).
		Run(func(mock.Arguments) {
			once.Do(func() { close(firstCall) })
		})

	return job, firstCall
}

func waitFor(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(eventuallyTimeout):
		t.Fatal(message)
	}
}

func TestNewGroupValidation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name    string
		logger  *slog.Logger
		config  worker.Config
		wantErr error
	}{
		{name: "nil logger", logger: nil, config: validConfig(), wantErr: worker.ErrNilLogger},
		{
			name:    "zero interval",
			logger:  logger,
			config:  worker.Config{JobTimeout: jobTimeout},
			wantErr: worker.ErrInvalidTickerInterval,
		},
		{
			name:    "negative interval",
			logger:  logger,
			config:  worker.Config{Interval: -tickerInterval, JobTimeout: jobTimeout},
			wantErr: worker.ErrInvalidTickerInterval,
		},
		{
			name:    "zero job timeout",
			logger:  logger,
			config:  worker.Config{Interval: tickerInterval},
			wantErr: worker.ErrInvalidJobTimeout,
		},
		{
			name:    "negative job timeout",
			logger:  logger,
			config:  worker.Config{Interval: tickerInterval, JobTimeout: -jobTimeout},
			wantErr: worker.ErrInvalidJobTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			group, err := worker.NewGroup(tt.logger, tt.config)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, group)
		})
	}
}

func TestNewGroupSuccess(t *testing.T) {
	t.Parallel()

	group, err := worker.NewGroup(slog.New(slog.DiscardHandler), validConfig())

	require.NoError(t, err)
	assert.NotNil(t, group)
}

func TestGroupAddValidation(t *testing.T) {
	t.Parallel()

	job := (&jobMock{}).Run

	tests := []struct {
		name    string
		jobName string
		job     worker.JobFunc
		opts    []worker.JobOption
		wantErr error
	}{
		{name: "empty job name", jobName: "", job: job, wantErr: worker.ErrEmptyJobName},
		{name: "blank job name", jobName: "   ", job: job, wantErr: worker.ErrEmptyJobName},
		{name: "nil job", jobName: jobName, job: nil, wantErr: worker.ErrNilJob},
		{
			name:    "zero interval override",
			jobName: jobName,
			job:     job,
			opts:    []worker.JobOption{worker.WithInterval(0)},
			wantErr: worker.ErrInvalidTickerInterval,
		},
		{
			name:    "negative interval override",
			jobName: jobName,
			job:     job,
			opts:    []worker.JobOption{worker.WithInterval(-tickerInterval)},
			wantErr: worker.ErrInvalidTickerInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			group, err := worker.NewGroup(slog.New(slog.DiscardHandler), validConfig())
			require.NoError(t, err)

			require.ErrorIs(t, group.Add(tt.jobName, tt.job, tt.opts...), tt.wantErr)
		})
	}
}

func TestGroupAddRejectsDuplicateName(t *testing.T) {
	t.Parallel()

	group, err := worker.NewGroup(slog.New(slog.DiscardHandler), validConfig())
	require.NoError(t, err)

	require.NoError(t, group.Add(jobName, (&jobMock{}).Run))

	require.ErrorIs(t, group.Add(jobName, (&jobMock{}).Run), worker.ErrDuplicateJobName)
}

func TestGroupAddAfterStart(t *testing.T) {
	t.Parallel()

	group, err := worker.NewGroup(slog.New(slog.DiscardHandler), validConfig())
	require.NoError(t, err)

	job, firstCall := signalJob(changedCount, nil)
	require.NoError(t, group.Add(jobName, job.Run))

	group.Start(t.Context())
	defer group.Stop()

	waitFor(t, firstCall, "group did not run the job")

	require.ErrorIs(t, group.Add(otherJobName, (&jobMock{}).Run), worker.ErrGroupStarted)
}

func TestGroupRun(t *testing.T) {
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

			group, err := worker.NewGroup(logger, validConfig())
			require.NoError(t, err)

			job, firstCall := signalJob(tt.count, tt.jobErr)
			require.NoError(t, group.Add(jobName, job.Run))

			group.Start(t.Context())

			waitFor(t, firstCall, "group did not run the job")

			group.Stop()

			logs := buf.String()
			if tt.wantLog != "" {
				assert.Contains(t, logs, tt.wantLog)
			}
			for _, unwanted := range tt.notWantLogs {
				assert.NotContains(t, logs, unwanted)
			}
			// Every line must carry the job name so concurrent workers stay
			// distinguishable in a shared log stream.
			assert.Contains(t, logs, `"job":"`+jobName+`"`)
			assert.Contains(t, logs, logStartedMessage)
			assert.Contains(t, logs, logStoppedMessage)
		})
	}
}

func TestGroupStopWaitsForEveryJob(t *testing.T) {
	t.Parallel()

	group, err := worker.NewGroup(slog.New(slog.DiscardHandler), validConfig())
	require.NoError(t, err)

	var running sync.WaitGroup
	running.Add(2)

	var mu sync.Mutex
	stopped := make(map[string]bool)

	// Blocks until its context ends, so Stop can only return once both
	// goroutines observed the cancellation and exited.
	blocking := func(name string) worker.JobFunc {
		var once sync.Once

		return func(ctx context.Context) (int64, error) {
			once.Do(running.Done)
			<-ctx.Done()

			mu.Lock()
			defer mu.Unlock()
			stopped[name] = true

			return 0, ctx.Err()
		}
	}

	require.NoError(t, group.Add(jobName, blocking(jobName)))
	require.NoError(t, group.Add(otherJobName, blocking(otherJobName)))

	group.Start(t.Context())

	done := make(chan struct{})
	go func() {
		running.Wait()
		close(done)
	}()
	waitFor(t, done, "jobs did not start")

	group.Stop()

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, stopped[jobName])
	assert.True(t, stopped[otherJobName])
}

func TestGroupStopIsSafeWithoutStart(t *testing.T) {
	t.Parallel()

	group, err := worker.NewGroup(slog.New(slog.DiscardHandler), validConfig())
	require.NoError(t, err)

	require.NoError(t, group.Add(jobName, (&jobMock{}).Run))

	assert.NotPanics(t, group.Stop)
	assert.NotPanics(t, group.Stop)
}

func TestGroupStartIsIdempotent(t *testing.T) {
	t.Parallel()

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	group, err := worker.NewGroup(logger, validConfig())
	require.NoError(t, err)

	job, firstCall := signalJob(changedCount, nil)
	require.NoError(t, group.Add(jobName, job.Run))

	ctx := t.Context()
	group.Start(ctx)
	group.Start(ctx)

	waitFor(t, firstCall, "group did not run the job")

	group.Stop()

	assert.Equal(t, 1, strings.Count(buf.String(), logStartedMessage))
}

func TestGroupWithIntervalOverridesTheDefault(t *testing.T) {
	t.Parallel()

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// A default no test could wait for, so only the override can explain a tick.
	group, err := worker.NewGroup(logger, worker.Config{Interval: time.Hour, JobTimeout: jobTimeout})
	require.NoError(t, err)

	job, firstCall := signalJob(changedCount, nil)
	require.NoError(t, group.Add(jobName, job.Run, worker.WithInterval(tickerInterval)))

	group.Start(t.Context())

	waitFor(t, firstCall, "group did not run the job")

	group.Stop()

	assert.Contains(t, buf.String(), `"interval":`+strconv.FormatInt(int64(tickerInterval), 10))
}

func TestGroupTimesOutASingleRun(t *testing.T) {
	t.Parallel()

	group, err := worker.NewGroup(slog.New(slog.DiscardHandler), worker.Config{
		Interval:   time.Hour,
		JobTimeout: tickerInterval,
	})
	require.NoError(t, err)

	deadline := make(chan time.Time, 1)
	require.NoError(t, group.Add(jobName, func(ctx context.Context) (int64, error) {
		<-ctx.Done()
		jobDeadline, ok := ctx.Deadline()
		require.True(t, ok)
		deadline <- jobDeadline

		return 0, ctx.Err()
	}))

	group.Start(t.Context())
	defer group.Stop()

	select {
	case <-deadline:
	case <-time.After(eventuallyTimeout):
		t.Fatal("job context was not cancelled by the job timeout")
	}
}
