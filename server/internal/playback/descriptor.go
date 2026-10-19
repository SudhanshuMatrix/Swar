package playback

import (
	"context"
	"sync"
	"time"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/observability"
)

// Descriptor tells the client how to play a track. Sources are references
// to authorized upstream streams that the client fetches directly.
type Descriptor struct {
	TrackID      string                `json:"trackId"`
	Available    bool                  `json:"available"`
	Reason       string                `json:"reason,omitempty"`
	Sources      []catalog.MediaSource `json:"sources"`
	ExpiresAt    *time.Time            `json:"expiresAt,omitempty"`
	RefreshAfter *time.Time            `json:"refreshAfter,omitempty"`
	Track        *catalog.Track        `json:"track,omitempty"`
}

// Resolver produces and caches descriptors. A descriptor is cached only
// until shortly before its sources expire and is never served stale.
type Resolver struct {
	cat   *catalog.Service
	cache *cache.Cache
	m     *observability.Metrics
	now   func() time.Time

	mu        sync.Mutex
	prefetchs map[string]inflight // owner -> in-flight prefetch
	seq       uint64
}

type inflight struct {
	cancel context.CancelFunc
	seq    uint64
}

func NewResolver(cat *catalog.Service, c *cache.Cache, m *observability.Metrics) *Resolver {
	return &Resolver{cat: cat, cache: c, m: m, now: time.Now, prefetchs: map[string]inflight{}}
}

const (
	refreshMargin = 2 * time.Minute
	maxDescTTL    = 30 * time.Minute
	unavailTTL    = 5 * time.Minute
)

// Resolve returns the descriptor for an internal track ID. Unavailability
// (feature disabled, restricted source) is a successful response with
// Available=false and a reason; transport problems are errors.
func (r *Resolver) Resolve(ctx context.Context, trackID string) (*Descriptor, error) {
	d, err := r.resolve(ctx, trackID)
	if err != nil {
		return nil, err
	}
	// Defensive: never hand out a cached descriptor past its refresh
	// point. Re-resolve once; a fresh descriptor is returned as is.
	if d.RefreshAfter != nil && !r.now().Before(*d.RefreshAfter) {
		r.Invalidate(trackID)
		return r.resolve(ctx, trackID)
	}
	return d, nil
}

func (r *Resolver) resolve(ctx context.Context, trackID string) (*Descriptor, error) {
	provider, pid, ok := catalog.ParseID(trackID)
	if !ok {
		return nil, catalog.Invalid("invalid track id")
	}
	id := catalog.MakeID(provider, pid)
	d, err := cache.GetOrLoad(ctx, r.cache, "desc:"+id, 30*time.Second, func(ctx context.Context) (*Descriptor, time.Duration, error) {
		r.m.Inc("playback_resolves_total")
		sources, exp, err := r.cat.MediaSources(ctx, id)
		if err != nil {
			switch catalog.KindOf(err) {
			case catalog.KindUnavailable, catalog.KindNotFound:
				msg := err.Error()
				if ce, ok := err.(*catalog.Error); ok {
					msg = ce.Message
				}
				return &Descriptor{TrackID: id, Available: false, Reason: msg, Sources: []catalog.MediaSource{}}, unavailTTL, nil
			}
			return nil, 0, err
		}
		now := r.now()
		refresh := exp.Add(-refreshMargin)
		ttl := min(refresh.Sub(now), maxDescTTL)
		if ttl <= 0 {
			ttl = 0 // already near expiry: return but do not cache
		}
		return &Descriptor{TrackID: id, Available: true, Sources: sources, ExpiresAt: &exp, RefreshAfter: &refresh}, ttl, nil
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Invalidate drops a cached descriptor (e.g. the client reported a 403).
func (r *Resolver) Invalidate(trackID string) {
	if provider, pid, ok := catalog.ParseID(trackID); ok {
		r.cache.Delete("desc:" + catalog.MakeID(provider, pid))
	}
}

// PrefetchResult pairs metadata with a descriptor (or the error message).
type PrefetchResult struct {
	TrackID    string         `json:"trackId"`
	Track      *catalog.Track `json:"track,omitempty"`
	Descriptor *Descriptor    `json:"descriptor,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// Prefetch resolves metadata and descriptors for the next one or two queue
// items. A new prefetch from the same owner cancels the previous one, so
// stale work from an old queue order stops. With dataSaver only the
// immediate next item is prepared.
func (r *Resolver) Prefetch(ctx context.Context, owner string, trackIDs []string, dataSaver bool) ([]PrefetchResult, error) {
	maxItems := 2
	if dataSaver {
		maxItems = 1
	}
	ids := dedupe(trackIDs)
	if len(ids) > maxItems {
		ids = ids[:maxItems]
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if owner != "" {
		r.mu.Lock()
		if prev, ok := r.prefetchs[owner]; ok {
			prev.cancel()
		}
		r.seq++
		mine := r.seq
		r.prefetchs[owner] = inflight{cancel: cancel, seq: mine}
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			// Only remove our own registration.
			if cur, ok := r.prefetchs[owner]; ok && cur.seq == mine {
				delete(r.prefetchs, owner)
			}
			r.mu.Unlock()
		}()
	}

	out := make([]PrefetchResult, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			res := PrefetchResult{TrackID: id}
			if t, err := r.cat.Track(ctx, id); err == nil {
				res.Track = t
			}
			d, err := r.Resolve(ctx, id)
			if err != nil {
				res.Error = errMessage(err)
			} else {
				res.Descriptor = d
			}
			out[i] = res
		}(i, id)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		r.m.Inc("prefetch_canceled_total")
		return nil, catalog.NewError(catalog.KindUnavailable, "prefetch was superseded by a newer request", err)
	}
	r.m.Inc("prefetch_total")
	return out, nil
}

func errMessage(err error) string {
	if ce, ok := err.(*catalog.Error); ok {
		return ce.Message
	}
	return "could not resolve playback"
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
