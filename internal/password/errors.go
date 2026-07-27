package password

import "errors"

// ErrTooManyConcurrentHashes is returned when the concurrent-hash budget
// (see maxConcurrentHashes) is exhausted. Argon2id is memory-heavy, and this
// type is shared by every caller in the process, so it bounds worst-case
// memory use regardless of who is hashing.
var ErrTooManyConcurrentHashes = errors.New("too many concurrent password hashes")
