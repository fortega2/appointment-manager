package password

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	semaphoreValidEncodedHash = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"
	semaphorePlainPassword    = "correct horse battery staple"

	// Short enough to keep the suite fast, long enough that a caller which
	// wrongly failed fast would still be measurably quicker.
	semaphoreSlotHoldTime = 50 * time.Millisecond
	semaphoreGiveUpWait   = 30 * time.Millisecond
)

// recordingMetrics counts the queue observations Argon2 emits.
type recordingMetrics struct {
	mu              sync.Mutex
	waits           int
	timedOut        int
	clientCancelled int
}

func (m *recordingMetrics) ObservePasswordQueueWait(context.Context, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.waits++
}

func (m *recordingMetrics) RecordPasswordQueueTimedOut() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.timedOut++
}

func (m *recordingMetrics) RecordPasswordQueueClientCancelled() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clientCancelled++
}

func (m *recordingMetrics) counts() (waits, timedOut, clientCancelled int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.waits, m.timedOut, m.clientCancelled
}

// newCheapArgon2 keeps the real semaphore but shrinks the KDF to a cost the
// slowest runner still finishes instantly. Queueing behaviour is independent of
// the cost parameters, so nothing under test is weakened.
func newCheapArgon2() *Argon2 {
	a := NewArgon2(nil)
	a.memory = 64
	a.iterations = 1

	return a
}

// saturate fills every slot and returns a release func for one of them.
func saturate(t *testing.T, a *Argon2) func() {
	t.Helper()

	for range maxConcurrentHashes {
		a.sem <- struct{}{}
	}

	return func() { <-a.sem }
}

func hashCall(ctx context.Context, a *Argon2) error {
	_, err := a.Hash(ctx, semaphorePlainPassword)

	return err
}

func compareCall(ctx context.Context, a *Argon2) error {
	_, err := a.Compare(ctx, semaphoreValidEncodedHash, "irrelevant")

	return err
}

func TestQueuesUntilSlotFrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(context.Context, *Argon2) error
	}{
		{name: "hash", call: hashCall},
		{name: "compare", call: compareCall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := NewArgon2(nil)
			release := saturate(t, a)

			go func() {
				time.Sleep(semaphoreSlotHoldTime)
				release()
			}()

			start := time.Now()
			err := tt.call(t.Context(), a)

			require.NoError(t, err)
			assert.GreaterOrEqual(t, time.Since(start), semaphoreSlotHoldTime,
				"must have waited for the slot instead of failing fast")
		})
	}
}

func TestGivesUpWhenSlotNeverFrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		call              func(context.Context, *Argon2) error
		ctx               func(t *testing.T) context.Context
		wantCtxErr        error
		wantTimedOut      int
		wantClientCancels int
	}{
		{
			name: "budget elapses",
			call: hashCall,
			ctx: func(t *testing.T) context.Context {
				t.Helper()

				// A parent deadline shorter than maxQueueWait wins, so the test
				// does not sit through the real budget.
				ctx, cancel := context.WithTimeout(t.Context(), semaphoreGiveUpWait)
				t.Cleanup(cancel)

				return ctx
			},
			wantCtxErr:   context.DeadlineExceeded,
			wantTimedOut: 1,
		},
		{
			name: "client hangs up",
			call: compareCall,
			ctx: func(t *testing.T) context.Context {
				t.Helper()

				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return ctx
			},
			wantCtxErr:        context.Canceled,
			wantClientCancels: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingMetrics{}
			a := NewArgon2(recorder)
			saturate(t, a)

			err := tt.call(tt.ctx(t), a)

			require.ErrorIs(t, err, ErrTooManyConcurrentHashes)
			require.ErrorIs(t, err, tt.wantCtxErr)

			waits, timedOut, clientCancels := recorder.counts()
			assert.Equal(t, tt.wantTimedOut, timedOut)
			assert.Equal(t, tt.wantClientCancels, clientCancels,
				"a client hanging up must not be reported as saturation")
			assert.Equal(t, 1, waits,
				"the wait must be observed even when no slot was granted, or the histogram hides the longest waits")
		})
	}
}

func TestConcurrentHashesAllSucceed(t *testing.T) {
	t.Parallel()

	const callers = 3 * maxConcurrentHashes

	// Real Argon2 cost would race the queue against maxQueueWait, making the
	// result a statement about the machine's KDF throughput rather than the
	// semaphore: on a slow CI runner one 64 MiB hash outlasts the whole budget.
	a := newCheapArgon2()

	errs := make([]error, callers)

	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			_, errs[i] = a.Hash(t.Context(), semaphorePlainPassword)
		})
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoErrorf(t, err, "caller %d was rejected", i)
	}
}

func TestAcquireRecordsWaitOnSuccess(t *testing.T) {
	t.Parallel()

	recorder := &recordingMetrics{}
	a := NewArgon2(recorder)

	_, err := a.Hash(t.Context(), semaphorePlainPassword)
	require.NoError(t, err)

	waits, timedOut, clientCancels := recorder.counts()
	assert.Equal(t, 1, waits)
	assert.Zero(t, timedOut)
	assert.Zero(t, clientCancels)
}

func TestHashAndCompareReleaseSemaphoreSlot(t *testing.T) {
	t.Parallel()

	a := NewArgon2(nil)

	hash, err := a.Hash(t.Context(), semaphorePlainPassword)
	require.NoError(t, err)
	assert.Empty(t, a.sem, "Hash must release its slot")

	_, err = a.Compare(t.Context(), hash, semaphorePlainPassword)
	require.NoError(t, err)
	assert.Empty(t, a.sem, "Compare must release its slot")
}
