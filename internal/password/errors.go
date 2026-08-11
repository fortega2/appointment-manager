package password

import "errors"

// ErrTooManyConcurrentHashes is returned when no hashing slot frees up before
// the caller's context ends or maxQueueWait elapses — not on the first busy
// moment. It wraps the underlying context error, so a client that disconnected
// mid-wait is indistinguishable here; the queue-timeout metric's reason label
// carries that distinction.
var ErrTooManyConcurrentHashes = errors.New("too many concurrent password hashes")
