package password

import "errors"

// ErrTooManyConcurrentHashes is returned when no hashing slot frees up before
// the caller's context ends or maxQueueWait elapses — not on the first busy
// moment. It wraps the underlying context error.
var ErrTooManyConcurrentHashes = errors.New("too many concurrent password hashes")
