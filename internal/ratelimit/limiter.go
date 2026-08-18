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
//
// The address is weighed first, and a request it refuses inserts nothing into
// the account store. That ordering is load-bearing (ADR 0009 Decision 5).
func (l *Limiter) Allow(address netip.Addr, account string) Decision {
	if !l.enabled {
		return Decision{Allowed: true}
	}

	now := l.now()
	key := keyForAccount(account)

	l.mu.Lock()
	defer l.mu.Unlock()

	addressBucket := l.bucketForAddress(address, now)

	addressDecision := addressBucket.decide(now, l.ipCfg)
	if !addressDecision.Allowed {
		l.metrics.RecordLoginRateLimitedByIP()

		return l.refusedByAddress(key, now, addressDecision)
	}

	accountBucket := l.bucketForAccount(key, now)

	accountDecision := accountBucket.decide(now, l.accountCfg)
	if !accountDecision.Allowed {
		l.metrics.RecordLoginRateLimitedByAccount()

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
//
// It returns the allowance as it stands after the credit, because the caller
// has already written headers describing the state before it — replacing them
// is what stops a successful login from reporting a budget it no longer has.
func (l *Limiter) RecordSuccess(address netip.Addr, account string) Decision {
	if !l.enabled {
		return Decision{Allowed: true}
	}

	now := l.now()
	key := keyForAccount(account)

	l.mu.Lock()
	defer l.mu.Unlock()

	accountBucket := l.bucketForAccount(key, now)
	addressBucket := l.bucketForAddress(address, now)

	accountBucket.fill(l.accountCfg)
	addressBucket.refund(l.ipCfg)

	return mostRestrictive(
		accountBucket.headroom(now, l.accountCfg),
		addressBucket.headroom(now, l.ipCfg),
	)
}

// RecordAbandoned gives back the token an attempt spent when the attempt never
// reached the password check at all — the account lookup failed, or no hashing
// slot was free. Nothing was learned about the credentials, so charging for it
// would let an outage ration a caller who did nothing wrong, and this
// application has no password-reset flow to escape that.
//
// Only the account is refunded, and by one token rather than refilled: the
// attempt proved nothing, so it earns its charge back and no more. The address
// keeps paying, because whoever is driving the load that emptied the hashing
// queue is exactly who the address budget exists to slow down.
//
// It returns the allowance as it stands after the refund, for the same reason
// RecordSuccess does: the caller has already written headers describing the
// state before it.
func (l *Limiter) RecordAbandoned(address netip.Addr, account string) Decision {
	if !l.enabled {
		return Decision{Allowed: true}
	}

	now := l.now()
	key := keyForAccount(account)

	l.mu.Lock()
	defer l.mu.Unlock()

	accountBucket := l.bucketForAccount(key, now)
	addressBucket := l.bucketForAddress(address, now)

	accountBucket.refund(l.accountCfg)

	return mostRestrictive(
		accountBucket.headroom(now, l.accountCfg),
		addressBucket.headroom(now, l.ipCfg),
	)
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

// refusedByAddress completes a decision the address bucket has already turned
// down. It reads the account bucket only when one is already tracked, so the
// refusal leaves the account store exactly as it found it.
func (l *Limiter) refusedByAddress(key accountKey, now time.Time, addressDecision Decision) Decision {
	tracked, ok := l.accounts.peek(key)
	if !ok {
		return addressDecision
	}

	accountDecision := tracked.decide(now, l.accountCfg)
	if !accountDecision.Allowed {
		l.metrics.RecordLoginRateLimitedByAccount()
	}

	return mostRestrictive(accountDecision, addressDecision)
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
