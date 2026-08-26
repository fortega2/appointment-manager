package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	phcPartsLen      = 6
	phcAlgoPartIndex = 1
	phcVersionIndex  = 2
	phcParamsIndex   = 3
	phcSaltIndex     = 4
	phcHashIndex     = 5

	defaultMemoryKiB    = 64 * 1024
	defaultIterations   = 3
	defaultParallelism  = 1
	defaultSaltLenBytes = 16
	defaultKeyLenBytes  = 32

	maxMemoryKiB     = 1024 * 1024
	maxIterations    = 10
	maxParallelism   = 8
	maxHashLenBytes  = 128
	minimumParamsVal = 1

	// maxConcurrentHashes bounds how many Hash/Compare calls may run at once.
	// The binding constraint is CPU, not memory: with defaultParallelism = 1,
	// two hashes already saturate the container's GOMAXPROCS=2
	// (docker/docker-compose.yml). Raising it buys timeslicing, not throughput.
	maxConcurrentHashes = 2

	// maxQueueWait bounds how long a caller waits for a slot. It must stay well
	// under the server's WriteTimeout (cmd/server/main.go), or a queued login
	// would still be waiting once its response can no longer be written.
	maxQueueWait = 3 * time.Second

	// The cost WithTestCost applies. Chosen so a hash stays in the tens of
	// milliseconds even on the CI runner, leaving maxQueueWait roughly an order
	// of magnitude of headroom, while still exercising real Argon2 work.
	testCostMemoryKiB  = 4 * 1024
	testCostIterations = 1
)

type Argon2 struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
	sem         chan struct{}
	metrics     Metrics
}

// Option customises a hasher before it starts serving callers.
type Option func(*Argon2)

// WithTestCost lowers the Argon2 cost far below the production defaults, and is
// for tests only -- it weakens every hash the returned hasher writes.
func WithTestCost() Option {
	return func(a *Argon2) {
		a.memory = testCostMemoryKiB
		a.iterations = testCostIterations
	}
}

// NewArgon2 builds a hasher whose concurrency budget is shared by every caller
// in the process. A nil hashMetrics disables instrumentation.
func NewArgon2(hashMetrics Metrics, opts ...Option) *Argon2 {
	if hashMetrics == nil {
		hashMetrics = noopMetrics{}
	}

	hasher := &Argon2{
		memory:      defaultMemoryKiB,
		iterations:  defaultIterations,
		parallelism: defaultParallelism,
		saltLen:     defaultSaltLenBytes,
		keyLen:      defaultKeyLenBytes,
		sem:         make(chan struct{}, maxConcurrentHashes),
		metrics:     hashMetrics,
	}

	for _, opt := range opts {
		opt(hasher)
	}

	return hasher
}

func (a *Argon2) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, a.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	if err := a.acquire(ctx); err != nil {
		return "", err
	}
	defer a.release()

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		a.iterations,
		a.memory,
		a.parallelism,
		a.keyLen,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		a.memory,
		a.iterations,
		a.parallelism,
		b64Salt,
		b64Hash,
	), nil
}

type parsedPHC struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	hash        []byte
}

func (a *Argon2) Compare(ctx context.Context, encodedHash, plainPassword string) (bool, error) {
	parsed, err := parsePHCEncodedHash(encodedHash)
	if err != nil {
		return false, err
	}

	hashLen := len(parsed.hash)
	if hashLen > maxHashLenBytes {
		return false, fmt.Errorf("hash length exceeds maximum: %d > %d", hashLen, maxHashLenBytes)
	}

	if err := a.acquire(ctx); err != nil {
		return false, err
	}
	defer a.release()

	calculatedHash := argon2.IDKey(
		[]byte(plainPassword),
		parsed.salt,
		parsed.iterations,
		parsed.memory,
		parsed.parallelism,
		uint32(hashLen),
	)

	return subtle.ConstantTimeCompare(parsed.hash, calculatedHash) == 1, nil
}

// acquire reserves one of the concurrent-hash slots, waiting up to maxQueueWait
// for one to free up.
func (a *Argon2) acquire(ctx context.Context) error {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, maxQueueWait)
	defer cancel()

	select {
	case a.sem <- struct{}{}:
		a.metrics.ObservePasswordQueueWait(ctx, time.Since(start))

		return nil
	case <-ctx.Done():
		// Observed on both arms: dropping the callers that waited longest is
		// what would make p95 look healthy under saturation. See ADR 0008.
		a.metrics.ObservePasswordQueueWait(ctx, time.Since(start))

		err := ctx.Err()
		a.recordWaitFailure(err)

		return fmt.Errorf("%w: %w", ErrTooManyConcurrentHashes, err)
	}
}

func (a *Argon2) release() {
	<-a.sem
}

func (a *Argon2) recordWaitFailure(err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		a.metrics.RecordPasswordQueueTimedOut()
		return
	}
	a.metrics.RecordPasswordQueueClientCancelled()
}

func parsePHCEncodedHash(encodedHash string) (*parsedPHC, error) {
	parts := strings.Split(encodedHash, "$")
	if err := validatePHCParts(parts); err != nil {
		return nil, err
	}

	memory, iterations, parallelism, err := parsePHCParams(parts[phcParamsIndex])
	if err != nil {
		return nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[phcSaltIndex])
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[phcHashIndex])
	if err != nil {
		return nil, fmt.Errorf("decode hash: %w", err)
	}

	return &parsedPHC{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		salt:        salt,
		hash:        decodedHash,
	}, nil
}

func validatePHCParts(parts []string) error {
	if len(parts) != phcPartsLen {
		return errors.New("invalid encoded hash format")
	}
	if parts[phcAlgoPartIndex] != "argon2id" {
		return errors.New("invalid algorithm")
	}

	var version int
	if _, err := fmt.Sscanf(parts[phcVersionIndex], "v=%d", &version); err != nil {
		return fmt.Errorf("parse version: %w", err)
	}
	if version != argon2.Version {
		return errors.New("incompatible argon2 version")
	}

	return nil
}

func parsePHCParams(raw string) (uint32, uint32, uint8, error) {
	var memory uint32
	var iterations uint32
	var parallelism uint8

	if _, err := fmt.Sscanf(raw, "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, fmt.Errorf("parse params: %w", err)
	}
	if memory < minimumParamsVal || memory > maxMemoryKiB {
		return 0, 0, 0, errors.New("invalid memory cost")
	}
	if iterations < minimumParamsVal || iterations > maxIterations {
		return 0, 0, 0, errors.New("invalid iteration cost")
	}
	if parallelism < minimumParamsVal || parallelism > maxParallelism {
		return 0, 0, 0, errors.New("invalid parallelism")
	}

	return memory, iterations, parallelism, nil
}
