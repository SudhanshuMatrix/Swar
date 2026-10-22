package search

import (
	"context"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/providers"
)

// FederatedSearch coordinates concurrent multi-provider queries with bounded concurrency,
// timeouts, deduplication, and ranking.
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

// SearchTracks queries all enabled providers concurrently, aggregates, deduplicates, and ranks.
func (f *FederatedSearch) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, map[string]error) {
	if limit <= 0 {
		limit = 25
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	var rawTracks []catalog.Track
	errs := make(map[string]error)

	for _, p := range f.providers {
		wg.Add(1)
		go func(prv providers.Provider) {
			defer wg.Done()
			results, err := prv.SearchTracks(ctx, query, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[prv.Name()] = err
			} else {
				rawTracks = append(rawTracks, results...)
			}
		}(p)
	}

	wg.Wait()

	// Deduplicate across providers into canonical records
	merged := DeduplicateAndMergeTracks(rawTracks)

	// Rank deterministically by relevance to query
	ranked := RankTracks(merged, query)

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	return ranked, errs
}

// SearchAlbums queries enabled providers concurrently for albums, deduplicating by normalized name.
func (f *FederatedSearch) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, map[string]error) {
	if limit <= 0 {
		limit = 25
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	var rawAlbums []catalog.Album
	errs := make(map[string]error)

	for _, p := range f.providers {
		wg.Add(1)
		go func(prv providers.Provider) {
			defer wg.Done()
			results, err := prv.SearchAlbums(ctx, query, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[prv.Name()] = err
			} else {
				rawAlbums = append(rawAlbums, results...)
			}
		}(p)
	}

	wg.Wait()

	// Deduplicate albums by normalized name and artist
	seen := make(map[string]bool)
	var deduped []catalog.Album
	for _, a := range rawAlbums {
		key := strings.ToLower(strings.TrimSpace(a.Name))
		if a.Artists != nil && len(a.Artists) > 0 {
			key += "|" + strings.ToLower(strings.TrimSpace(a.Artists[0].Name))
		}
		if key != "" && !seen[key] {
			seen[key] = true
			deduped = append(deduped, a)
		}
	}

	if len(deduped) > limit {
		deduped = deduped[:limit]
	}

	return deduped, errs
}

// SearchArtists queries enabled providers concurrently for artists.
func (f *FederatedSearch) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, map[string]error) {
	if limit <= 0 {
		limit = 25
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	var rawArtists []catalog.ArtistRef
	errs := make(map[string]error)

	for _, p := range f.providers {
		wg.Add(1)
		go func(prv providers.Provider) {
			defer wg.Done()
			results, err := prv.SearchArtists(ctx, query, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[prv.Name()] = err
			} else {
				rawArtists = append(rawArtists, results...)
			}
		}(p)
	}

	wg.Wait()

	// Deduplicate artists by normalized name
	seen := make(map[string]bool)
	var deduped []catalog.ArtistRef
	for _, a := range rawArtists {
		key := strings.ToLower(strings.TrimSpace(a.Name))
		if key != "" && !seen[key] {
			seen[key] = true
			deduped = append(deduped, a)
		}
	}

	if len(deduped) > limit {
		deduped = deduped[:limit]
	}

	return deduped, errs
}

// SearchPlaylists queries enabled providers concurrently for playlists.
func (f *FederatedSearch) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, map[string]error) {
	if limit <= 0 {
		limit = 25
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	var rawPlaylists []catalog.Playlist
	errs := make(map[string]error)

	for _, p := range f.providers {
		wg.Add(1)
		go func(prv providers.Provider) {
			defer wg.Done()
			results, err := prv.SearchPlaylists(ctx, query, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[prv.Name()] = err
			} else {
				rawPlaylists = append(rawPlaylists, results...)
			}
		}(p)
	}

	wg.Wait()

	seen := make(map[string]bool)
	var deduped []catalog.Playlist
	for _, pl := range rawPlaylists {
		key := strings.ToLower(strings.TrimSpace(pl.Name))
		if key != "" && !seen[key] {
			seen[key] = true
			deduped = append(deduped, pl)
		}
	}

	if len(deduped) > limit {
		deduped = deduped[:limit]
	}

	return deduped, errs
}
