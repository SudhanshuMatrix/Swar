package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Group coalesces concurrent work on the same key, like GetOrLoad, with one
// difference: the shared work is cancelled once every caller waiting for it
// has given up. Use it for expensive work that nobody should pay for after
// the people who asked have gone, such as an external process.
//
// A caller that gives up gets its own ctx error; the remaining callers keep
// waiting. The work's context keeps the first caller's values (but not its
// cancellation) and is bounded by the timeout passed to Do.
type Group struct {
	mu        sync.Mutex
	calls     map[string]*call
	coalesced atomic.Int64
	canceled  atomic.Int64
}

type call struct {
	done    chan struct{}
	value   any
	err     error
	waiters int
	cancel  context.CancelFunc
}

// Coalesced reports how many callers joined work already in progress.
func (g *Group) Coalesced() int64 { return g.coalesced.Load() }

// Canceled reports how many shared runs were stopped because every caller
// left before they finished.
func (g *Group) Canceled() int64 { return g.canceled.Load() }

// InFlight reports the number of keys with work in progress.
func (g *Group) InFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

// Do runs fn once for all concurrent callers with the same key.
func Do[T any](ctx context.Context, g *Group, key string, timeout time.Duration, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*call{}
	}
	c, joined := g.calls[key]
	if joined {
		c.waiters++
		g.coalesced.Add(1)
	} else {
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		c = &call{done: make(chan struct{}), waiters: 1, cancel: cancel}
		g.calls[key] = c
		go func() {
			v, err := fn(wctx)
			cancel()
			c.value, c.err = v, err
			g.mu.Lock()
			if g.calls[key] == c {
				delete(g.calls, key)
			}
			g.mu.Unlock()
			close(c.done)
		}()
	}
	g.mu.Unlock()

	select {
	case <-c.done:
		if c.err != nil {
			return zero, c.err
		}
		v, _ := c.value.(T)
		return v, nil
	case <-ctx.Done():
		g.mu.Lock()
		c.waiters--
		if c.waiters == 0 {
			select {
			case <-c.done:
			default:
				// Nobody is left to use the result: stop the work, and let
				// the next caller start afresh instead of joining it.
				c.cancel()
				g.canceled.Add(1)
				if g.calls[key] == c {
					delete(g.calls, key)
				}
			}
		}
		g.mu.Unlock()
		return zero, ctx.Err()
	}
}

// GetOrLoadShared returns the cached value for key, or loads it through g.
// Like GetOrLoad, errors are never cached and a zero TTL returns the value
// without caching it. Unlike GetOrLoad, the load stops when every caller
// waiting for it has gone.
func GetOrLoadShared[T any](ctx context.Context, c *Cache, g *Group, key string, timeout time.Duration, load Loader[T]) (T, error) {
	if v, ok := c.Get(key); ok {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}
	return Do(ctx, g, key, timeout, func(ctx context.Context) (T, error) {
		// Another caller may have filled the cache between our miss and
		// this run starting.
		if v, ok := c.Peek(key); ok {
			if t, ok := v.(T); ok {
				return t, nil
			}
		}
		c.loads.Add(1)
		v, ttl, err := load(ctx)
		if err != nil {
			c.loadErrors.Add(1)
			return v, err
		}
		c.Set(key, v, ttl)
		return v, nil
	})
}

// Peek returns a live value without counting a hit or miss or changing its
// place in the LRU order.
func (c *Cache) Peek(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if !c.now().Before(e.expires) {
		return nil, false
	}
	return e.value, true
}
