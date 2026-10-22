package search

import (
	"context"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/providers"
)

// mainTimeout bounds main search: it must feel instant, so a provider that
// hasn't answered by then is left out rather than waited for.
const mainTimeout = 3 * time.Second

// FederatedSearch coordinates concurrent multi-provider queries with
// timeouts, deduplication and ranking.
//
// Swar's main search is JioSaavn and YouTube Music; these providers feed
// Deep Search, which asks all of them (SearchTracks). The "main" methods
// (Spotify first, then Deezer and iTunes) remain for using the engine as a
// primary catalog.
type FederatedSearch struct {
	providers []providers.Provider
	timeout   time.Duration
}

func NewFederatedSearch(prvs []providers.Provider, timeout time.Duration) *FederatedSearch {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &FederatedSearch{
		providers: prvs,
		timeout:   timeout,
	}
}

func enabled(p providers.Provider) bool {
	if en, ok := p.(interface{ Enabled() bool }); ok {
		return en.Enabled()
	}
	return true
}

// mainProviders returns the providers main search asks first, and the ones
// it falls back to.
func (f *FederatedSearch) mainProviders() (first, fallback []providers.Provider) {
	var fast []providers.Provider
	for _, p := range f.providers {
		if !enabled(p) {
			continue
		}
		switch p.Name() {
		case catalog.ProviderSpotify:
			first = append(first, p)
		case catalog.ProviderDeezer, catalog.ProviderITunes:
			fast = append(fast, p)
		}
	}
	if len(fast) == 0 {
		fast = f.deepProviders()
	}
	if len(first) == 0 {
		return fast, nil
	}
	return first, fast
}

// deepProviders is every enabled provider.
func (f *FederatedSearch) deepProviders() []providers.Provider {
	var out []providers.Provider
	for _, p := range f.providers {
		if enabled(p) {
			out = append(out, p)
		}
	}
	return out
}

// gather runs fn on every provider concurrently and collects the results.
func gather[T any](ctx context.Context, prvs []providers.Provider, timeout time.Duration,
	fn func(context.Context, providers.Provider) ([]T, error)) ([]T, map[string]error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []T
	errs := make(map[string]error)
	for _, p := range prvs {
		wg.Add(1)
		go func(prv providers.Provider) {
			defer wg.Done()
			results, err := fn(ctx, prv)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[prv.Name()] = err
			} else {
				out = append(out, results...)
			}
		}(p)
	}
	wg.Wait()
	return out, errs
}

// gatherMain asks the main providers and, only when they find nothing,
// the fallback ones.
func gatherMain[T any](ctx context.Context, f *FederatedSearch,
	fn func(context.Context, providers.Provider) ([]T, error)) ([]T, map[string]error) {
	first, fallback := f.mainProviders()
	out, errs := gather(ctx, first, mainTimeout, fn)
	if len(out) > 0 || len(fallback) == 0 {
		return out, errs
	}
	more, moreErrs := gather(ctx, fallback, mainTimeout, fn)
	for k, v := range moreErrs {
		errs[k] = v
	}
	return more, errs
}

// SearchTracksFast is main song search.
func (f *FederatedSearch) SearchTracksFast(ctx context.Context, query string, limit int) ([]catalog.Track, map[string]error) {
	if limit <= 0 {
		limit = 50
	}
	raw, errs := gatherMain(ctx, f, func(ctx context.Context, p providers.Provider) ([]catalog.Track, error) {
		return p.SearchTracks(ctx, query, limit)
	})
	return rankLimit(raw, query, limit), errs
}

// SearchTracks is Deep Search's catalog part: every enabled provider,
// merged and ranked.
func (f *FederatedSearch) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, map[string]error) {
	if limit <= 0 {
		limit = 50
	}
	raw, errs := gather(ctx, f.deepProviders(), f.timeout, func(ctx context.Context, p providers.Provider) ([]catalog.Track, error) {
		return p.SearchTracks(ctx, query, limit)
	})
	return rankLimit(raw, query, limit), errs
}

func rankLimit(raw []catalog.Track, query string, limit int) []catalog.Track {
	ranked := RankTracks(DeduplicateAndMergeTracks(raw), query)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// SearchAlbums is main album search.
func (f *FederatedSearch) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, map[string]error) {
	if limit <= 0 {
		limit = 25
	}
	raw, errs := gatherMain(ctx, f, func(ctx context.Context, p providers.Provider) ([]catalog.Album, error) {
		return p.SearchAlbums(ctx, query, limit)
	})
	return dedupe(raw, limit, func(a catalog.Album) string {
		key := strings.ToLower(strings.TrimSpace(a.Name))
		if len(a.Artists) > 0 {
			key += "|" + strings.ToLower(strings.TrimSpace(a.Artists[0].Name))
		}
		return key
	}), errs
}

// SearchArtists is main singer search.
func (f *FederatedSearch) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, map[string]error) {
	if limit <= 0 {
		limit = 25
	}
	raw, errs := gatherMain(ctx, f, func(ctx context.Context, p providers.Provider) ([]catalog.ArtistRef, error) {
		return p.SearchArtists(ctx, query, limit)
	})
	return dedupe(raw, limit, func(a catalog.ArtistRef) string { return strings.ToLower(strings.TrimSpace(a.Name)) }), errs
}

// SearchPlaylists is main playlist search.
func (f *FederatedSearch) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, map[string]error) {
	if limit <= 0 {
		limit = 25
	}
	raw, errs := gatherMain(ctx, f, func(ctx context.Context, p providers.Provider) ([]catalog.Playlist, error) {
		return p.SearchPlaylists(ctx, query, limit)
	})
	return dedupe(raw, limit, func(pl catalog.Playlist) string { return strings.ToLower(strings.TrimSpace(pl.Name)) }), errs
}

// dedupe keeps the first item per non-empty key, up to limit items.
func dedupe[T any](in []T, limit int, key func(T) string) []T {
	seen := make(map[string]bool)
	var out []T
	for _, v := range in {
		k := key(v)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
		if len(out) == limit {
			break
		}
	}
	return out
}
