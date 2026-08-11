package password

import "errors"

// ErrTooManyConcurrentHashes is returned when no hashing slot frees up before
// the caller's context ends or maxQueueWait elapses — not on the first busy
// moment. It wraps the underlying context error, so callers can tell a wait
// that timed out from one whose client disconnected.
var ErrTooManyConcurrentHashes = errors.New("too many concurrent password hashes")
