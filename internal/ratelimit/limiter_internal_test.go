package ratelimit

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	account       = "Assistant@Example.com "
	otherAccount  = "other@example.com"
	accountBurst  = 3
	accountRefill = time.Minute
	ipBurst       = 5
	ipRefill      = 30 * time.Second
	maxEntries    = 8
)

var (
	reference = time.Date(2026, time.August, 15, 11, 0, 0, 0, time.UTC)
	address   = netip.MustParseAddr("203.0.113.7")
	otherAddr = netip.MustParseAddr("198.51.100.4")
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: reference}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

type recordingMetrics struct {
	mu        sync.Mutex
	byAccount int
	byIP      int
	evicted   int
}

func (m *recordingMetrics) RecordLoginRateLimitedByAccount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byAccount++
}

func (m *recordingMetrics) RecordLoginRateLimitedByIP() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byIP++
}

func (m *recordingMetrics) RecordLoginRateLimitEvicted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evicted++
}

func (m *recordingMetrics) counts() (int, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.byAccount, m.byIP, m.evicted
}

func testConfig() Config {
	return Config{
		Enabled:       true,
		AccountBurst:  accountBurst,
		AccountRefill: accountRefill,
		IPBurst:       ipBurst,
		IPRefill:      ipRefill,
		MaxEntries:    maxEntries,
	}
}

func newTestLimiter(t *testing.T) (*Limiter, *clock, *recordingMetrics) {
	t.Helper()

	fake := newClock()
	recorder := &recordingMetrics{}
	limiter, err := newWithClock(testConfig(), fake.Now, recorder)
	require.NoError(t, err)

	return limiter, fake, recorder
}

func TestNewRejectsAnInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(cfg *Config)
		expected error
	}{
		{name: "zero account burst", mutate: func(cfg *Config) { cfg.AccountBurst = 0 }, expected: ErrInvalidAccountBurst},
		{name: "negative account refill", mutate: func(cfg *Config) { cfg.AccountRefill = -time.Second }, expected: ErrInvalidAccountRefill},
		{name: "zero ip burst", mutate: func(cfg *Config) { cfg.IPBurst = 0 }, expected: ErrInvalidIPBurst},
		{name: "zero ip refill", mutate: func(cfg *Config) { cfg.IPRefill = 0 }, expected: ErrInvalidIPRefill},
		{name: "zero max entries", mutate: func(cfg *Config) { cfg.MaxEntries = 0 }, expected: ErrInvalidMaxEntries},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			tt.mutate(&cfg)

			limiter, err := New(cfg, nil)

			require.Error(t, err)
			assert.Nil(t, limiter)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestNewValidatesEvenWhenDisabled(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Enabled = false
	cfg.AccountBurst = 0

	limiter, err := New(cfg, nil)

	require.ErrorIs(t, err, ErrInvalidAccountBurst)
	assert.Nil(t, limiter)
}

func TestAllowSpendsTheAccountBurstThenRefuses(t *testing.T) {
	t.Parallel()

	limiter, _, recorder := newTestLimiter(t)

	for i := range accountBurst {
		decision := limiter.Allow(address, account)

		require.Truef(t, decision.Allowed, "attempt %d should be within the burst", i)
		assert.Equal(t, accountBurst, decision.Limit)
		assert.Equal(t, accountBurst-i-1, decision.Remaining)
		assert.Zero(t, decision.RetryAfter)
	}

	decision := limiter.Allow(address, account)

	assert.False(t, decision.Allowed)
	assert.Equal(t, 0, decision.Remaining)
	assert.Equal(t, accountRefill, decision.RetryAfter)
	assert.Equal(t, accountBurst*accountRefill, decision.Reset)

	byAccount, byIP, _ := recorder.counts()
	assert.Equal(t, 1, byAccount)
	assert.Equal(t, 0, byIP)
}

func TestAllowChargesNothingForARefusedAttempt(t *testing.T) {
	t.Parallel()

	limiter, fake, _ := newTestLimiter(t)

	for range accountBurst {
		require.True(t, limiter.Allow(address, account).Allowed)
	}

	// Hammering while over the limit must not push the recovery further out:
	// a bucket drained by the requests it is refusing would never lift.
	for range 20 {
		require.False(t, limiter.Allow(address, account).Allowed)
	}

	fake.advance(accountRefill)

	decision := limiter.Allow(address, account)

	assert.True(t, decision.Allowed, "one refill interval must buy exactly one attempt back")
	assert.False(t, limiter.Allow(address, account).Allowed)
}

func TestAllowDoesNotChargeTheAccountWhenTheAddressIsRefused(t *testing.T) {
	t.Parallel()

	limiter, _, recorder := newTestLimiter(t)

	// Spend the address budget across accounts that are each well inside their
	// own, so only the address bucket can be the one that runs out.
	for i := range ipBurst {
		require.True(t, limiter.Allow(address, fmt.Sprintf("user%d@example.com", i)).Allowed)
	}

	refused := limiter.Allow(address, account)
	require.False(t, refused.Allowed)
	assert.Equal(t, ipBurst, refused.Limit, "the address bucket is the one that refused")
	assert.Equal(t, ipRefill, refused.RetryAfter)

	// The account named by that refused attempt keeps its full budget, or one
	// blocked address could lock out every account it cared to name.
	fresh := limiter.Allow(otherAddr, account)

	assert.True(t, fresh.Allowed)
	assert.Equal(t, accountBurst-1, fresh.Remaining)

	byAccount, byIP, _ := recorder.counts()
	assert.Equal(t, 0, byAccount)
	assert.Equal(t, 1, byIP)
}

func TestAllowReportsTheMostRestrictiveBucket(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	decision := limiter.Allow(address, account)

	require.True(t, decision.Allowed)
	assert.Equal(t, accountBurst, decision.Limit, "the account has less headroom than the address")
	assert.Equal(t, accountBurst-1, decision.Remaining)
}

func TestAllowKeysAccountsCaseAndSpaceInsensitively(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	require.True(t, limiter.Allow(address, "  USER@example.com  ").Allowed)

	decision := limiter.Allow(address, "user@EXAMPLE.com")

	assert.Equal(t, accountBurst-2, decision.Remaining, "padding must not buy a second budget")
}

func TestRecordSuccessRefillsTheAccountAndRefundsTheAddress(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	for range accountBurst {
		require.True(t, limiter.Allow(address, account).Allowed)
	}
	require.False(t, limiter.Allow(address, account).Allowed)

	limiter.RecordSuccess(address, account)

	decision := limiter.Allow(address, account)

	assert.True(t, decision.Allowed, "a user who finally gets it right must not stay rationed")
	assert.Equal(t, accountBurst-1, decision.Remaining)
}

func TestRecordSuccessLeavesOtherAccountsAlone(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	require.True(t, limiter.Allow(address, otherAccount).Allowed)
	limiter.RecordSuccess(address, account)

	decision := limiter.Allow(address, otherAccount)

	assert.Equal(t, accountBurst-2, decision.Remaining)
}

func TestRecordAbandonedLetsARationedAccountBackIn(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	for range accountBurst {
		require.True(t, limiter.Allow(address, account).Allowed)
	}
	require.False(t, limiter.Allow(address, account).Allowed)

	refunded := limiter.RecordAbandoned(address, account)

	assert.Equal(t, 1, refunded.Remaining, "the refund is one token, not a refill")
	assert.True(t,
		limiter.Allow(address, account).Allowed,
		"an attempt that never reached the password check must not cost the caller its turn")
}

func TestRecordAbandonedLeavesTheAddressPaying(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	// A distinct account each time, so the address is the only budget that can
	// bind: what is under test is that refunding does not hand it back a token.
	for i := range ipBurst {
		named := fmt.Sprintf("user%d@example.com", i)

		require.True(t, limiter.Allow(address, named).Allowed)
		limiter.RecordAbandoned(address, named)
	}

	decision := limiter.Allow(address, "one-more@example.com")

	assert.False(t, decision.Allowed, "the address budget must still run out under a refunded flood")
}

func TestDisabledLimiterAllowsEverythingAndAdvertisesNothing(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Enabled = false
	limiter, err := New(cfg, nil)
	require.NoError(t, err)

	for range 3 * (accountBurst + ipBurst) {
		decision := limiter.Allow(address, account)

		require.True(t, decision.Allowed)
		assert.False(t, decision.Enforced())
	}

	limiter.RecordSuccess(address, account)
	assert.False(t, limiter.RecordAbandoned(address, account).Enforced())

	deleted, err := limiter.DeleteExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, 0, limiter.TrackedAccounts())
}

func TestEnforcedDistinguishesALimitFromNone(t *testing.T) {
	t.Parallel()

	assert.True(t, Decision{Limit: 1}.Enforced())
	assert.False(t, Decision{}.Enforced())
}

func TestDeleteExpiredDropsOnlyTheEntriesThatCarryNothing(t *testing.T) {
	t.Parallel()

	limiter, fake, _ := newTestLimiter(t)

	require.True(t, limiter.Allow(address, account).Allowed)
	require.Equal(t, 1, limiter.TrackedAccounts())
	require.Equal(t, 1, limiter.TrackedAddresses())

	deleted, err := limiter.DeleteExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted, "a spent bucket still carries information")

	fake.advance(accountBurst * accountRefill)

	deleted, err = limiter.DeleteExpired(t.Context())

	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted, "one account entry and one address entry")
	assert.Equal(t, 0, limiter.TrackedAccounts())
	assert.Equal(t, 0, limiter.TrackedAddresses())
}

func TestAllowEvictsAtTheEntryCapAndSaysSo(t *testing.T) {
	t.Parallel()

	limiter, _, recorder := newTestLimiter(t)

	// Spread across addresses so every attempt is granted: only a granted
	// attempt reaches the account store, so only granted attempts can fill it.
	for i := range maxEntries + 2 {
		from := netip.AddrFrom4([4]byte{203, 0, 113, byte(i / 2)})
		require.True(t, limiter.Allow(from, fmt.Sprintf("user%d@example.com", i)).Allowed)
	}

	assert.Equal(t, maxEntries, limiter.TrackedAccounts(), "the cap is what bounds memory")

	_, _, evicted := recorder.counts()
	assert.Equal(t, 2, evicted)
}

func TestAllowRefusedByTheAddressLeavesTheAccountStoreAlone(t *testing.T) {
	t.Parallel()

	limiter, _, recorder := newTestLimiter(t)

	// Drain the address across accounts that each stay inside their own budget.
	for i := range ipBurst {
		require.True(t, limiter.Allow(address, fmt.Sprintf("user%d@example.com", i)).Allowed)
	}
	require.Equal(t, ipBurst, limiter.TrackedAccounts())

	// A blocked address hammering invented names must not be able to insert,
	// because every insert past the cap evicts a real account's drained bucket
	// and hands it a fresh one.
	for i := range 10 * maxEntries {
		require.False(t, limiter.Allow(address, fmt.Sprintf("invented%d@example.com", i)).Allowed)
	}

	assert.Equal(t, ipBurst, limiter.TrackedAccounts(), "a refused attempt must track nothing")

	_, _, evicted := recorder.counts()
	assert.Zero(t, evicted, "a blocked address must not evict the accounts it never reached")
}

func TestAllowRefusedByTheAddressStillReportsATrackedAccountsRefusal(t *testing.T) {
	t.Parallel()

	limiter, _, recorder := newTestLimiter(t)

	for range accountBurst {
		require.True(t, limiter.Allow(address, account).Allowed)
	}
	for i := range ipBurst - accountBurst {
		require.True(t, limiter.Allow(address, fmt.Sprintf("user%d@example.com", i)).Allowed)
	}

	// Both are spent now, and the account's wait is the longer of the two.
	decision := limiter.Allow(address, account)

	require.False(t, decision.Allowed)
	assert.Equal(t, accountBurst, decision.Limit, "the account is the more restrictive of the two")
	assert.Equal(t, accountRefill, decision.RetryAfter)

	byAccount, byIP, _ := recorder.counts()
	assert.Equal(t, 1, byAccount)
	assert.Equal(t, 1, byIP)
}

func TestAllowIsSafeUnderConcurrentUse(t *testing.T) {
	t.Parallel()

	limiter, _, _ := newTestLimiter(t)

	const callers = 50

	var wg sync.WaitGroup
	start := make(chan struct{})
	allowed := make([]bool, callers)

	for i := range callers {
		wg.Go(func() {
			<-start

			allowed[i] = limiter.Allow(address, account).Allowed
		})
	}
	close(start)
	wg.Wait()

	granted := 0
	for _, ok := range allowed {
		if ok {
			granted++
		}
	}

	assert.Equal(t, accountBurst, granted, "the burst must be handed out exactly once, whatever the interleaving")
}
