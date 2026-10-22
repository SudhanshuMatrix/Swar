package music

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/providers"
	"swar/internal/music/providers/deezer"
	"swar/internal/music/providers/itunes"
	"swar/internal/music/providers/musicbrainz"
	"swar/internal/music/providers/spotify"
	"swar/internal/music/search"
)

// EngineOptions configures the native music metadata engine.
type EngineOptions struct {
	DeezerEnabled      bool
	ITunesEnabled      bool
	MusicBrainzEnabled bool
	SpotifyEnabled     bool

	SpotifyClientID     string
	SpotifyClientSecret string

	SearchTimeout  time.Duration
	MaxConcurrency int
	ClientOptions  providers.ClientOptions
}

// Engine implements catalog.Provider, uniting all metadata providers
// into a federated, zero-key, high-performance music engine.
type Engine struct {
	opts      EngineOptions
	providers map[string]providers.Provider
	prvList   []providers.Provider
	federated *search.FederatedSearch
}

func NewEngine(opts EngineOptions) *Engine {
	if opts.SearchTimeout <= 0 {
		opts.SearchTimeout = 8 * time.Second
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 8
	}

	prvMap := make(map[string]providers.Provider)
	var prvList []providers.Provider

	// 1. Deezer (zero-key, rich catalog, 30s preview)
	if opts.DeezerEnabled {
		dz := deezer.New(opts.ClientOptions)
		prvMap[dz.Name()] = dz
		prvList = append(prvList, dz)
	}

	// 2. Apple iTunes Search API (zero-key, outstanding Indian music coverage, high-res art)
	if opts.ITunesEnabled {
		it := itunes.New(opts.ClientOptions)
		prvMap[it.Name()] = it
		prvList = append(prvList, it)
	}

	// 3. MusicBrainz (zero-key open encyclopedia, ISRCs, recordings)
	if opts.MusicBrainzEnabled {
		mb := musicbrainz.New(opts.ClientOptions)
		prvMap[mb.Name()] = mb
		prvList = append(prvList, mb)
	}

	// 4. Spotify (optional client credentials)
	if opts.SpotifyEnabled && opts.SpotifyClientID != "" && opts.SpotifyClientSecret != "" {
		sp := spotify.New(spotify.Options{
			ClientOptions: opts.ClientOptions,
			ClientID:      opts.SpotifyClientID,
			ClientSecret:  opts.SpotifyClientSecret,
		})
		prvMap[sp.Name()] = sp
		prvList = append(prvList, sp)
	}

	return &Engine{
		opts:      opts,
		providers: prvMap,
		prvList:   prvList,
		federated: search.NewFederatedSearch(prvList, opts.SearchTimeout),
	}
}

// Name returns the primary provider name.
func (e *Engine) Name() string {
	return catalog.ProviderMusic
}

// Supports reports whether this engine can resolve the given provider prefix.
func (e *Engine) Supports(provider string) bool {
	switch provider {
	case catalog.ProviderMusic, catalog.ProviderDeezer, catalog.ProviderITunes,
		catalog.ProviderMusicBrainz, catalog.ProviderSpotify, catalog.ProviderSaavn:
		return true
	}
	_, ok := e.providers[provider]
	return ok
}

func (e *Engine) SearchSongs(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Track], error) {
	if limit <= 0 {
		limit = 50
	}
	tracks, _ := e.federated.SearchTracksFast(ctx, q, limit)
	start := page * limit
	return &catalog.Page[catalog.Track]{
		Total:   len(tracks),
		Start:   start,
		Page:    page,
		Limit:   limit,
		Results: tracks,
	}, nil
}

func (e *Engine) SearchSongsDeep(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Track], error) {
	if limit <= 0 {
		limit = 50
	}
	tracks, _ := e.federated.SearchTracks(ctx, q, limit)
	start := page * limit
	return &catalog.Page[catalog.Track]{
		Total:   len(tracks),
		Start:   start,
		Page:    page,
		Limit:   limit,
		Results: tracks,
	}, nil
}

func (e *Engine) SearchAlbums(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Album], error) {
	albums, _ := e.federated.SearchAlbums(ctx, q, limit)
	start := page * limit
	return &catalog.Page[catalog.Album]{
		Total:   len(albums),
		Start:   start,
		Page:    page,
		Limit:   limit,
		Results: albums,
	}, nil
}

func (e *Engine) SearchArtists(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.ArtistRef], error) {
	artists, _ := e.federated.SearchArtists(ctx, q, limit)
	start := page * limit
	return &catalog.Page[catalog.ArtistRef]{
		Total:   len(artists),
		Start:   start,
		Page:    page,
		Limit:   limit,
		Results: artists,
	}, nil
}

func (e *Engine) SearchPlaylists(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Playlist], error) {
	playlists, _ := e.federated.SearchPlaylists(ctx, q, limit)
	start := page * limit
	return &catalog.Page[catalog.Playlist]{
		Total:   len(playlists),
		Start:   start,
		Page:    page,
		Limit:   limit,
		Results: playlists,
	}, nil
}

// Tracks fetches tracks across any enabled provider.
func (e *Engine) Tracks(ctx context.Context, ids []string) ([]catalog.Track, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	tracks := make([]catalog.Track, 0, len(ids))

	for _, fullID := range ids {
		provider, pid, ok := catalog.ParseID(fullID)
		if !ok {
			provider = e.defaultProviderForID(fullID)
			pid = fullID
		}

		// Handle legacy Saavn IDs gracefully without crashing
		if provider == catalog.ProviderSaavn {
			t := catalog.Track{
				ID:          catalog.MakeID(catalog.ProviderSaavn, pid),
				Provider:    catalog.ProviderSaavn,
				ProviderID:  pid,
				Title:       "Legacy Track (" + pid + ")",
				DurationSec: 0,
				Playable:    false,
				Download:    catalog.DownloadEligibility{Eligible: false, Reason: "legacy_source_migrated"},
			}
			mu.Lock()
			tracks = append(tracks, t)
			mu.Unlock()
			continue
		}

		prv, exists := e.providers[provider]
		if !exists {
			continue
		}

		wg.Add(1)
		go func(p providers.Provider, rawID string) {
			defer wg.Done()
			if tr, err := p.Track(ctx, rawID); err == nil && tr != nil {
				mu.Lock()
				tracks = append(tracks, *tr)
				mu.Unlock()
			}
		}(prv, pid)
	}

	wg.Wait()
	return tracks, nil
}

func (e *Engine) defaultProviderForID(id string) string {
	// If numeric ID, typically Deezer or iTunes
	if _, err := strconv.ParseInt(id, 10, 64); err == nil {
		if _, ok := e.providers[catalog.ProviderDeezer]; ok {
			return catalog.ProviderDeezer
		}
		if _, ok := e.providers[catalog.ProviderITunes]; ok {
			return catalog.ProviderITunes
		}
	}
	// If UUID format (contains hyphens), typically MusicBrainz
	if strings.Count(id, "-") == 4 {
		return catalog.ProviderMusicBrainz
	}
	return catalog.ProviderDeezer
}

func (e *Engine) TrackByLink(ctx context.Context, token string) ([]catalog.Track, error) {
	// Parse link if it's a URL
	if u, err := url.Parse(token); err == nil && u.Host != "" {
		if strings.Contains(u.Host, "deezer.com") {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 && parts[0] == "track" {
				if t, err := e.Track(ctx, catalog.MakeID(catalog.ProviderDeezer, parts[1])); err == nil {
					return []catalog.Track{*t}, nil
				}
			}
		}
	}
	// Fallback to title search
	p, err := e.SearchSongs(ctx, token, 0, 5)
	if err != nil {
		return nil, err
	}
	return p.Results, nil
}

func (e *Engine) Track(ctx context.Context, id string) (*catalog.Track, error) {
	ts, err := e.Tracks(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, catalog.NotFound("track not found")
	}
	return &ts[0], nil
}

func (e *Engine) Related(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	if prv, ok := e.providers[provider]; ok {
		// Use artist top tracks or related lookups
		t, err := prv.Track(ctx, pid)
		if err == nil && t != nil && len(t.Artists) > 0 {
			artID := strings.TrimPrefix(t.Artists[0].ID, provider+":")
			top, err := prv.ArtistTopTracks(ctx, artID, limit)
			if err == nil && len(top) > 0 {
				return top, nil
			}
		}
	}

	// Fallback across first available provider
	for _, p := range e.prvList {
		top, err := p.ArtistTopTracks(ctx, pid, limit)
		if err == nil && len(top) > 0 {
			return top, nil
		}
	}

	return []catalog.Track{}, nil
}

func (e *Engine) Album(ctx context.Context, id string) (*catalog.Album, error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	if prv, ok := e.providers[provider]; ok {
		return prv.Album(ctx, pid)
	}

	for _, p := range e.prvList {
		if a, err := p.Album(ctx, pid); err == nil && a != nil {
			return a, nil
		}
	}
	return nil, catalog.NotFound("album not found")
}

func (e *Engine) AlbumByLink(ctx context.Context, token string) (*catalog.Album, error) {
	p, err := e.SearchAlbums(ctx, token, 0, 1)
	if err != nil || len(p.Results) == 0 {
		return nil, catalog.NotFound("album not found")
	}
	return &p.Results[0], nil
}

func (e *Engine) Artist(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Artist, error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	var a *catalog.Artist
	if prv, ok := e.providers[provider]; ok {
		if res, err := prv.Artist(ctx, pid); err == nil && res != nil {
			a = res
		}
	}

	if a == nil {
		for _, p := range e.prvList {
			if res, err := p.Artist(ctx, pid); err == nil && res != nil {
				a = res
				break
			}
		}
	}
	if a == nil {
		return nil, catalog.NotFound("artist not found")
	}

	// Backfill artist images if missing
	if len(a.Images) == 0 {
		if len(a.TopSongs) > 0 && len(a.TopSongs[0].Images) > 0 {
			a.Images = a.TopSongs[0].Images
		} else {
			if dz, ok := e.providers[catalog.ProviderDeezer]; ok {
				if refs, err := dz.SearchArtists(ctx, a.Name, 1); err == nil && len(refs) > 0 && len(refs[0].Image) > 0 {
					a.Images = refs[0].Image
				}
			}
			if len(a.Images) == 0 {
				if sp, ok := e.providers[catalog.ProviderSpotify]; ok {
					if en, ok := sp.(interface{ Enabled() bool }); !ok || en.Enabled() {
						if refs, err := sp.SearchArtists(ctx, a.Name, 1); err == nil && len(refs) > 0 && len(refs[0].Image) > 0 {
							a.Images = refs[0].Image
						}
					}
				}
			}
		}
	}

	return a, nil
}

func (e *Engine) ArtistByLink(ctx context.Context, token string, o catalog.ArtistOptions) (*catalog.Artist, error) {
	p, err := e.SearchArtists(ctx, token, 0, 1)
	if err != nil || len(p.Results) == 0 {
		return nil, catalog.NotFound("artist not found")
	}
	return e.Artist(ctx, p.Results[0].ID, o)
}

func (e *Engine) ArtistSongs(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Page[catalog.Track], error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	limit := o.SongCount
	if limit <= 0 {
		limit = 50
	}

	if prv, ok := e.providers[provider]; ok {
		tracks, err := prv.ArtistTopTracks(ctx, pid, limit)
		if err == nil {
			return &catalog.Page[catalog.Track]{
				Total:   len(tracks),
				Page:    o.Page,
				Limit:   limit,
				Results: tracks,
			}, nil
		}
	}

	return &catalog.Page[catalog.Track]{Page: o.Page, Limit: limit, Results: []catalog.Track{}}, nil
}

func (e *Engine) ArtistAlbums(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Page[catalog.Album], error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	limit := o.AlbumCount
	if limit <= 0 {
		limit = 50
	}

	if prv, ok := e.providers[provider]; ok {
		albs, err := prv.ArtistAlbums(ctx, pid, limit)
		if err == nil {
			return &catalog.Page[catalog.Album]{
				Total:   len(albs),
				Page:    o.Page,
				Limit:   limit,
				Results: albs,
			}, nil
		}
	}

	return &catalog.Page[catalog.Album]{Page: o.Page, Limit: limit, Results: []catalog.Album{}}, nil
}

func (e *Engine) Playlist(ctx context.Context, id string, page, limit int) (*catalog.Playlist, error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	if prv, ok := e.providers[provider]; ok {
		return prv.Playlist(ctx, pid, limit)
	}

	for _, p := range e.prvList {
		if pl, err := p.Playlist(ctx, pid, limit); err == nil && pl != nil {
			return pl, nil
		}
	}
	return nil, catalog.NotFound("playlist not found")
}

func (e *Engine) PlaylistByLink(ctx context.Context, token string, page, limit int) (*catalog.Playlist, error) {
	p, err := e.SearchPlaylists(ctx, token, page, limit)
	if err != nil || len(p.Results) == 0 {
		return nil, catalog.NotFound("playlist not found")
	}
	return &p.Results[0], nil
}

func (e *Engine) Lyrics(ctx context.Context, id string) (*catalog.Lyrics, error) {
	return nil, catalog.Unavailable("lyrics not supplied by metadata providers")
}

func (e *Engine) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	if dz, ok := e.providers[catalog.ProviderDeezer]; ok {
		return dz.Trending(ctx, language)
	}
	return []catalog.Collection{}, nil
}

func (e *Engine) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	if dz, ok := e.providers[catalog.ProviderDeezer]; ok {
		return dz.Browse(ctx, language)
	}
	return []catalog.Collection{}, nil
}

func (e *Engine) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	provider, pid, ok := catalog.ParseID(id)
	if !ok {
		provider = e.defaultProviderForID(id)
		pid = id
	}

	if provider == catalog.ProviderSaavn {
		return nil, time.Time{}, catalog.Unavailable("legacy saavn streaming is discontinued; use deep search audio relay")
	}

	if prv, ok := e.providers[provider]; ok {
		if msPrv, ok := prv.(interface {
			MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error)
		}); ok {
			return msPrv.MediaSources(ctx, pid)
		}
	}

	return nil, time.Time{}, catalog.Unavailable("no media source available for this provider")
}

// Providers lists the prefixes of the enabled providers (dz, it, mb, sp).
func (e *Engine) Providers() []string {
	out := make([]string, 0, len(e.prvList))
	for _, p := range e.prvList {
		out = append(out, p.Name())
	}
	return out
}

// Source adapts the engine to one provider prefix, so the catalog can look
// up Deep Search songs (and their previews) by ID.
func (e *Engine) Source(provider string) catalog.TrackSource { return engineSource{e, provider} }

type engineSource struct {
	e        *Engine
	provider string
}

func (s engineSource) Track(ctx context.Context, providerID string) (*catalog.Track, error) {
	return s.e.Track(ctx, catalog.MakeID(s.provider, providerID))
}

func (s engineSource) MediaSources(ctx context.Context, providerID string) ([]catalog.MediaSource, time.Time, error) {
	return s.e.MediaSources(ctx, catalog.MakeID(s.provider, providerID))
}
