// Package ratelimit throttles login attempts in memory, keyed by both the
// account being tried and the address trying it.
//
// The two keys answer different attacks and neither replaces the other: an
// address key stops one host working through many accounts, and an account key
// stops many hosts converging on one account — the distributed credential
// stuffing that an edge proxy, which can only see addresses, is blind to.
// See ADR 0009.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Decision is everything a handler needs to answer the request: whether to go
// on, and the numbers the RateLimit headers carry. A zero Limit means no limit
// applies and the handler should write no headers at all.
type Decision struct {
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	Reset      time.Duration
	Allowed    bool
}

// Enforced reports whether a limit was actually applied, and so whether the
// response should advertise one.
func (d Decision) Enforced() bool {
	return d.Limit > 0
}

// Config is the limiter's tuning. Refill durations are the time to earn back a
// single token, not the length of a window.
type Config struct {
	AccountBurst  int
	AccountRefill time.Duration
	IPBurst       int
	IPRefill      time.Duration
	MaxEntries    int
	Enabled       bool
}

// accountKey is a digest rather than the address itself. The address arrives
// from a request body bounded only by the handler's byte limit, and it would
// otherwise become a map key of that size; hashing pins every key to 32 bytes
// and keeps the address out of the limiter's state. It is not a security
// boundary — anyone holding the address can recompute it.
type accountKey [sha256.Size]byte

// Limiter hands out token-bucket decisions for login attempts. It is safe for
// concurrent use.
type Limiter struct {
	accountCfg bucketConfig
	ipCfg      bucketConfig
	metrics    Metrics
	mu         sync.Mutex
	accounts   *store[accountKey]
	addresses  *store[netip.Addr]
	now        func() time.Time
	enabled    bool
}

// New builds a limiter from cfg. A nil limitMetrics disables instrumentation.
// The configuration is validated even when disabled, so turning the limiter on
// later cannot surface a misconfiguration that was there all along.
func New(cfg Config, limitMetrics Metrics) (*Limiter, error) {
	return newWithClock(cfg, time.Now, limitMetrics)
}

func newWithClock(cfg Config, now func() time.Time, limitMetrics Metrics) (*Limiter, error) {
	switch {
	case cfg.AccountBurst <= 0:
		return nil, ErrInvalidAccountBurst
	case cfg.AccountRefill <= 0:
		return nil, ErrInvalidAccountRefill
	case cfg.IPBurst <= 0:
		return nil, ErrInvalidIPBurst
	case cfg.IPRefill <= 0:
		return nil, ErrInvalidIPRefill
	case cfg.MaxEntries <= 0:
		return nil, ErrInvalidMaxEntries
	}

	if now == nil {
		now = time.Now
	}
	if limitMetrics == nil {
		limitMetrics = noopMetrics{}
	}

	return &Limiter{
		enabled:    cfg.Enabled,
		accountCfg: bucketConfig{burst: float64(cfg.AccountBurst), refill: cfg.AccountRefill},
		ipCfg:      bucketConfig{burst: float64(cfg.IPBurst), refill: cfg.IPRefill},
		accounts:   newStore[accountKey](cfg.MaxEntries),
		addresses:  newStore[netip.Addr](cfg.MaxEntries),
		now:        now,
		metrics:    limitMetrics,
	}, nil
}

// Allow charges one attempt against both the account and the address, and
// reports whether the caller may proceed.
//
// A refused attempt costs nothing. Charging it would let a caller who is
// already over the limit hold the bucket empty with the very requests it is
// being refused, so the limit would never lift. For the same reason both
// buckets are charged or neither is: draining the account's budget on behalf of
// an address that was going to be refused anyway would turn one blocked
// attacker into a lockout of every account it names.
func (l *Limiter) Allow(address netip.Addr, account string) Decision {
	if !l.enabled {
		return Decision{Allowed: true}
	}

	now := l.now()
	key := keyForAccount(account)

	l.mu.Lock()
	defer l.mu.Unlock()

	accountBucket := l.bucketForAccount(key, now)
	addressBucket := l.bucketForAddress(address, now)

	accountDecision := accountBucket.decide(now, l.accountCfg)
	addressDecision := addressBucket.decide(now, l.ipCfg)

	if !accountDecision.Allowed || !addressDecision.Allowed {
		if !accountDecision.Allowed {
			l.metrics.RecordLoginRateLimitedByAccount()
		}
		if !addressDecision.Allowed {
			l.metrics.RecordLoginRateLimitedByIP()
		}

		return mostRestrictive(accountDecision, addressDecision)
	}

	accountBucket.consume()
	addressBucket.consume()

	// The headers describe what is left once this attempt is paid for, but the
	// verdict stays the one reached above.
	return mostRestrictive(
		accountBucket.headroom(now, l.accountCfg),
		addressBucket.headroom(now, l.ipCfg),
	)
}

// RecordSuccess credits a proven-legitimate attempt: the account's budget goes
// back to full and the address gets its token back. The account is refilled
// rather than merely refunded so that a user who mistyped their way to the
// limit is not left rationed once they get it right.
func (l *Limiter) RecordSuccess(address netip.Addr, account string) {
	if !l.enabled {
		return
	}

	now := l.now()
	key := keyForAccount(account)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.bucketForAccount(key, now).fill(l.accountCfg)
	l.bucketForAddress(address, now).refund(l.ipCfg)
}

// DeleteExpired drops the entries whose bucket has refilled completely and
// reports how many went. Its signature is worker.JobFunc, so it can run as a
// periodic sweep. This is hygiene, not the memory bound — Config.MaxEntries is
// what bounds memory, and it holds with or without this running.
func (l *Limiter) DeleteExpired(context.Context) (int64, error) {
	if !l.enabled {
		return 0, nil
	}

	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	return l.accounts.deleteFull(now, l.accountCfg) + l.addresses.deleteFull(now, l.ipCfg), nil
}

// TrackedAccounts reports how many accounts currently hold a bucket. It exists
// for the metrics gauge, which reads it from the collector's goroutine.
func (l *Limiter) TrackedAccounts() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.accounts.len()
}

// TrackedAddresses reports how many addresses currently hold a bucket.
func (l *Limiter) TrackedAddresses() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.addresses.len()
}

func (l *Limiter) bucketForAccount(key accountKey, now time.Time) *bucket {
	tracked, evicted := l.accounts.get(key, now, l.accountCfg)
	if evicted {
		l.metrics.RecordLoginRateLimitEvicted()
	}

	return tracked
}

func (l *Limiter) bucketForAddress(address netip.Addr, now time.Time) *bucket {
	tracked, evicted := l.addresses.get(address, now, l.ipCfg)
	if evicted {
		l.metrics.RecordLoginRateLimitEvicted()
	}

	return tracked
}

// keyForAccount folds case and trims space so that padding or capitalising the
// address cannot buy a second budget for the same account.
func keyForAccount(account string) accountKey {
	return sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
}

// mostRestrictive picks the decision the caller should be shown when the two
// buckets disagree: the one that refused, or, failing that, the one with less
// headroom. Reporting the roomier of the two would advertise budget the caller
// cannot actually spend.
func mostRestrictive(account, address Decision) Decision {
	switch {
	case account.Allowed != address.Allowed:
		if account.Allowed {
			return address
		}

		return account
	case account.RetryAfter != address.RetryAfter:
		if account.RetryAfter > address.RetryAfter {
			return account
		}

		return address
	case account.Remaining <= address.Remaining:
		return account
	default:
		return address
	}
}
