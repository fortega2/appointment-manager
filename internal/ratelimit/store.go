package ratelimit

import (
	"container/list"
	"time"
)

// entry pairs a bucket with its key so eviction can reach the map from the
// recency list.
type entry[K comparable] struct {
	key    K
	bucket bucket
}

// store is an LRU map of buckets. The cap is what bounds memory: the set of
// keys is chosen by whoever is sending requests, so an unbounded map would be
// the denial of service it exists to prevent. The periodic sweep is only
// hygiene on top of it.
type store[K comparable] struct {
	entries map[K]*list.Element
	order   *list.List
	max     int
}

func newStore[K comparable](maxEntries int) *store[K] {
	return &store[K]{
		entries: make(map[K]*list.Element),
		order:   list.New(),
		max:     maxEntries,
	}
}

// get returns the key's bucket, creating a full one if the key is new. It
// reports whether an entry had to be evicted to make room.
func (s *store[K]) get(key K, now time.Time, cfg bucketConfig) (*bucket, bool) {
	if element, ok := s.entries[key]; ok {
		s.order.MoveToFront(element)

		return &element.Value.(*entry[K]).bucket, false
	}

	evicted := false
	if len(s.entries) >= s.max {
		s.evictOldest()
		evicted = true
	}

	fresh := &entry[K]{key: key, bucket: bucket{tokens: cfg.burst, last: now}}
	s.entries[key] = s.order.PushFront(fresh)

	return &fresh.bucket, evicted
}

func (s *store[K]) evictOldest() {
	oldest := s.order.Back()
	if oldest == nil {
		return
	}

	s.order.Remove(oldest)
	delete(s.entries, oldest.Value.(*entry[K]).key)
}

// deleteFull drops every entry whose bucket has refilled completely and reports
// how many went. Such an entry is indistinguishable from the one a new caller
// would be handed, so forgetting it changes no decision.
func (s *store[K]) deleteFull(now time.Time, cfg bucketConfig) int64 {
	var deleted int64

	for element := s.order.Back(); element != nil; {
		previous := element.Prev()

		tracked := element.Value.(*entry[K])
		if tracked.bucket.full(now, cfg) {
			s.order.Remove(element)
			delete(s.entries, tracked.key)
			deleted++
		}

		element = previous
	}

	return deleted
}

func (s *store[K]) len() int {
	return len(s.entries)
}

// peek returns the key's bucket only if it is already tracked, creating nothing
// and disturbing no recency.
func (s *store[K]) peek(key K) (*bucket, bool) {
	element, ok := s.entries[key]
	if !ok {
		return nil, false
	}

	return &element.Value.(*entry[K]).bucket, true
}
