// Package cache implements a bounded, TTL-aware LRU cache with request
// coalescing. Concurrent misses for the same key share one load, so a burst
// of identical requests produces a single upstream call.
package cache

import (
	"container/list"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type entry struct {
	key     string
	value   any
	expires time.Time
}

// Stats are cumulative counters, safe to read at any time.
type Stats struct {
	Hits, Misses, Evictions, Loads, LoadErrors, Coalesced int64
	Size                                                  int
}

// Cache is a size-bounded LRU with per-entry expiry.
type Cache struct {
	mu    sync.Mutex
	max   int
	ll    *list.List
	items map[string]*list.Element
	now   func() time.Time

	flightMu sync.Mutex
	flights  map[string]*flight

	hits, misses, evictions, loads, loadErrors, coalesced atomic.Int64
}

type flight struct {
	done  chan struct{}
	value any
	err   error
}

// New returns a cache holding at most maxEntries items.
func New(maxEntries int) *Cache {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &Cache{
		max:     maxEntries,
		ll:      list.New(),
		items:   map[string]*list.Element{},
		now:     time.Now,
		flights: map[string]*flight{},
	}
}

// SetClock overrides the time source (tests only).
func (c *Cache) SetClock(now func() time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

// Get returns a live value for key.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	e := el.Value.(*entry)
	if !c.now().Before(e.expires) {
		c.removeElement(el)
		c.misses.Add(1)
		return nil, false
	}
	c.ll.MoveToFront(el)
	c.hits.Add(1)
	return e.value, true
}

// Set stores value for ttl. A non-positive ttl is a no-op.
func (c *Cache) Set(key string, value any, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	exp := c.now().Add(ttl)
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		e.value, e.expires = value, exp
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&entry{key: key, value: value, expires: exp})
	for c.ll.Len() > c.max {
		c.removeElement(c.ll.Back())
		c.evictions.Add(1)
	}
}

// Delete removes key.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
}

// DeletePrefix removes every key that starts with prefix and returns the
// number removed.
func (c *Cache) DeletePrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, el := range c.items {
		if strings.HasPrefix(k, prefix) {
			c.removeElement(el)
			n++
		}
	}
	return n
}

// Clear removes every entry and returns how many there were. Loads in
// progress are not interrupted; their results are stored as usual.
func (c *Cache) Clear() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.ll.Len()
	c.ll.Init()
	c.items = map[string]*list.Element{}
	return n
}

func (c *Cache) removeElement(el *list.Element) {
	c.ll.Remove(el)
	delete(c.items, el.Value.(*entry).key)
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

func (c *Cache) Stats() Stats {
	return Stats{
		Hits: c.hits.Load(), Misses: c.misses.Load(), Evictions: c.evictions.Load(),
		Loads: c.loads.Load(), LoadErrors: c.loadErrors.Load(), Coalesced: c.coalesced.Load(),
		Size: c.Len(),
	}
}

// Loader produces a value and the TTL it may be cached for. A zero TTL
// means "return it but do not cache it".
type Loader[T any] func(ctx context.Context) (T, time.Duration, error)

// GetOrLoad returns the cached value for key or runs load exactly once for
// all concurrent callers. Errors are never cached.
//
// The shared load runs on a context detached from any single caller's
// cancellation (bounded by loadTimeout) so one impatient client cannot fail
// the request for everybody waiting on the same key. Each caller still
// stops waiting when its own ctx is done.
func GetOrLoad[T any](ctx context.Context, c *Cache, key string, loadTimeout time.Duration, load Loader[T]) (T, error) {
	var zero T
	if v, ok := c.Get(key); ok {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}

	c.flightMu.Lock()
	f, inFlight := c.flights[key]
	if !inFlight {
		f = &flight{done: make(chan struct{})}
		c.flights[key] = f
	}
	c.flightMu.Unlock()

	if inFlight {
		c.coalesced.Add(1)
	} else {
		c.loads.Add(1)
		go func() {
			lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
			defer cancel()
			v, ttl, err := load(lctx)
			if err != nil {
				c.loadErrors.Add(1)
			} else {
				c.Set(key, v, ttl)
			}
			f.value, f.err = v, err
			c.flightMu.Lock()
			delete(c.flights, key)
			c.flightMu.Unlock()
			close(f.done)
		}()
	}

	select {
	case <-f.done:
		if f.err != nil {
			return zero, f.err
		}
		v, _ := f.value.(T)
		return v, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}
