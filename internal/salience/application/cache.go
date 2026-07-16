package application

import (
	"container/list"
	"sync"
	"time"
)

// decisionCache is a small mutex-guarded LRU with TTL keyed by a request
// fingerprint. It absorbs webhook redeliveries so a duplicate delivery does
// not re-spend tokens. In-memory only — re-spend after a restart is
// acceptable (decisions are cheap; duplicate deliveries are rare).
type decisionCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	entries  map[string]*list.Element
	order    *list.List // front = most recently used
}

type cacheEntry struct {
	key      string
	value    any
	storedAt time.Time
}

func newDecisionCache(capacity int, ttl time.Duration) *decisionCache {
	return &decisionCache{capacity: capacity, ttl: ttl, entries: map[string]*list.Element{}, order: list.New()}
}

func (c *decisionCache) get(key string, now time.Time) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry := element.Value.(*cacheEntry)
	if now.Sub(entry.storedAt) > c.ttl {
		c.order.Remove(element)
		delete(c.entries, key)
		return nil, false
	}
	c.order.MoveToFront(element)
	return entry.value, true
}

func (c *decisionCache) put(key string, value any, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		entry := element.Value.(*cacheEntry)
		entry.value = value
		entry.storedAt = now
		c.order.MoveToFront(element)
		return
	}
	c.entries[key] = c.order.PushFront(&cacheEntry{key: key, value: value, storedAt: now})
	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*cacheEntry).key)
	}
}
