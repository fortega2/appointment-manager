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
	mu      sync.Mutex
	waits   []time.Duration
	reasons []string
}

func (m *recordingMetrics) ObservePasswordQueueWait(_ context.Context, waited time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.waits = append(m.waits, waited)
}

func (m *recordingMetrics) RecordPasswordQueueTimeout(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reasons = append(m.reasons, reason)
}

func (m *recordingMetrics) recordedReasons() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]string(nil), m.reasons...)
}

func (m *recordingMetrics) waitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.waits)
}

// saturate fills every slot and returns a release func for one of them.
func saturate(t *testing.T, a *Argon2) func() {
	t.Helper()

	for range maxConcurrentHashes {
		a.sem <- struct{}{}
	}

	return func() { <-a.sem }
}

func TestHashQueuesUntilSlotFrees(t *testing.T) {
	t.Parallel()

	a := NewArgon2(nil)
	release := saturate(t, a)

	go func() {
		time.Sleep(semaphoreSlotHoldTime)
		release()
	}()

	start := time.Now()
	hash, err := a.Hash(t.Context(), semaphorePlainPassword)

	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.GreaterOrEqual(t, time.Since(start), semaphoreSlotHoldTime,
		"Hash must have waited for the slot instead of failing fast")
}

func TestCompareQueuesUntilSlotFrees(t *testing.T) {
	t.Parallel()

	a := NewArgon2(nil)
	release := saturate(t, a)

	go func() {
		time.Sleep(semaphoreSlotHoldTime)
		release()
	}()

	start := time.Now()
	_, err := a.Compare(t.Context(), semaphoreValidEncodedHash, "irrelevant")

	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), semaphoreSlotHoldTime,
		"Compare must have waited for the slot instead of failing fast")
}

func TestConcurrentHashesAllSucceed(t *testing.T) {
	t.Parallel()

	// 3x oversubscription: enough to prove queueing, few enough that the last
	// caller's wait stays clear of maxQueueWait on slow CI hardware.
	const callers = 3 * maxConcurrentHashes

	a := NewArgon2(nil)

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

func TestAcquireGivesUpWhenSaturated(t *testing.T) {
	t.Parallel()

	recorder := &recordingMetrics{}
	a := NewArgon2(recorder)
	saturate(t, a)

	// A parent deadline shorter than maxQueueWait wins, so the test does not
	// have to sit through the real budget.
	ctx, cancel := context.WithTimeout(t.Context(), semaphoreGiveUpWait)
	defer cancel()

	hash, err := a.Hash(ctx, semaphorePlainPassword)

	assert.Empty(t, hash)
	require.ErrorIs(t, err, ErrTooManyConcurrentHashes)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, []string{waitFailureTimeout}, recorder.recordedReasons())
}

func TestAcquireReturnsOnCancelledContext(t *testing.T) {
	t.Parallel()

	recorder := &recordingMetrics{}
	a := NewArgon2(recorder)
	saturate(t, a)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := a.Compare(ctx, semaphoreValidEncodedHash, "irrelevant")

	require.ErrorIs(t, err, ErrTooManyConcurrentHashes)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{waitFailureClientCancelled}, recorder.recordedReasons(),
		"a client hanging up must not be reported as saturation")
}

func TestAcquireRecordsWaitOnSuccess(t *testing.T) {
	t.Parallel()

	recorder := &recordingMetrics{}
	a := NewArgon2(recorder)

	_, err := a.Hash(t.Context(), semaphorePlainPassword)

	require.NoError(t, err)
	assert.Equal(t, 1, recorder.waitCount())
	assert.Empty(t, recorder.recordedReasons())
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
