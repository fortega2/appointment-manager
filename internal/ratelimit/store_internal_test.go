package ratelimit

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	storeBurst  = 2
	storeRefill = time.Minute
	storeMax    = 3
)

var storeReference = time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)

func storeTestConfig() bucketConfig {
	return bucketConfig{burst: storeBurst, refill: storeRefill}
}

func TestStoreGetCreatesAFullBucketAndThenReturnsIt(t *testing.T) {
	t.Parallel()

	cfg := storeTestConfig()
	tracked := newStore[string](storeMax)

	first, evicted := tracked.get("a", storeReference, cfg)
	require.False(t, evicted)
	assert.InDelta(t, float64(storeBurst), first.tokens, 0.0001)

	first.consume()

	second, evicted := tracked.get("a", storeReference, cfg)
	assert.False(t, evicted)
	assert.Same(t, first, second, "the same key must keep the same bucket")
	assert.InDelta(t, float64(storeBurst-1), second.tokens, 0.0001)
	assert.Equal(t, 1, tracked.len())
}

func TestStoreEvictsTheLeastRecentlyUsedAtCapacity(t *testing.T) {
	t.Parallel()

	cfg := storeTestConfig()
	tracked := newStore[string](storeMax)

	for i := range storeMax {
		_, evicted := tracked.get(strconv.Itoa(i), storeReference, cfg)
		require.False(t, evicted)
	}
	require.Equal(t, storeMax, tracked.len())

	// Touching "0" makes "1" the oldest, so it is the one that goes.
	_, _ = tracked.get("0", storeReference, cfg)

	_, evicted := tracked.get("fresh", storeReference, cfg)

	assert.True(t, evicted)
	assert.Equal(t, storeMax, tracked.len(), "the cap is what bounds memory")
	assert.Contains(t, tracked.entries, "0")
	assert.NotContains(t, tracked.entries, "1")
	assert.Contains(t, tracked.entries, "fresh")
}

func TestStoreDeleteFullDropsOnlyRefilledEntries(t *testing.T) {
	t.Parallel()

	cfg := storeTestConfig()
	tracked := newStore[string](storeMax)

	untouched, _ := tracked.get("untouched", storeReference, cfg)
	require.InDelta(t, float64(storeBurst), untouched.tokens, 0.0001)

	spent, _ := tracked.get("spent", storeReference, cfg)
	spent.consume()
	spent.consume()

	deleted := tracked.deleteFull(storeReference, cfg)

	assert.Equal(t, int64(1), deleted)
	assert.NotContains(t, tracked.entries, "untouched")
	assert.Contains(t, tracked.entries, "spent", "a drained bucket still carries information")

	// Once enough time has passed for it to refill, it carries nothing either.
	deleted = tracked.deleteFull(storeReference.Add(storeBurst*storeRefill), cfg)

	assert.Equal(t, int64(1), deleted)
	assert.Equal(t, 0, tracked.len())
}

func TestStoreDeleteFullOnAnEmptyStore(t *testing.T) {
	t.Parallel()

	tracked := newStore[string](storeMax)

	assert.Equal(t, int64(0), tracked.deleteFull(storeReference, storeTestConfig()))
}
