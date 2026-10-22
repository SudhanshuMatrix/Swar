package catalog

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"swar/internal/cache"
	"swar/internal/music/innertube"
)

// Provider is implemented by a catalog upstream (currently Saavn). All IDs
// passed to a Provider are provider IDs, not Swar internal IDs.
type Provider interface {
	Name() string
	SearchSongs(ctx context.Context, q string, page, limit int) (*Page[Track], error)
	SearchAlbums(ctx context.Context, q string, page, limit int) (*Page[Album], error)
	SearchArtists(ctx context.Context, q string, page, limit int) (*Page[ArtistRef], error)
	SearchPlaylists(ctx context.Context, q string, page, limit int) (*Page[Playlist], error)
	Tracks(ctx context.Context, ids []string) ([]Track, error)
	TrackByLink(ctx context.Context, token string) ([]Track, error)
	Related(ctx context.Context, id string, limit int) ([]Track, error)
	Album(ctx context.Context, id string) (*Album, error)
	AlbumByLink(ctx context.Context, token string) (*Album, error)
	Artist(ctx context.Context, id string, o ArtistOptions) (*Artist, error)
	ArtistByLink(ctx context.Context, token string, o ArtistOptions) (*Artist, error)
	ArtistSongs(ctx context.Context, id string, o ArtistOptions) (*Page[Track], error)
	ArtistAlbums(ctx context.Context, id string, o ArtistOptions) (*Page[Album], error)
	Playlist(ctx context.Context, id string, page, limit int) (*Playlist, error)
	PlaylistByLink(ctx context.Context, token string, page, limit int) (*Playlist, error)
	Lyrics(ctx context.Context, id string) (*Lyrics, error)
	Trending(ctx context.Context, language string) ([]Collection, error)
	Browse(ctx context.Context, language string) ([]Collection, error)
	// MediaSources returns authorized stream references and when they
	// expire. Providers return a KindUnavailable error when disabled.
	MediaSources(ctx context.Context, id string) ([]MediaSource, time.Time, error)
}

// TrackSource resolves single tracks for providers that are not full
// catalogs (e.g. deep-search candidates).
type TrackSource interface {
	Track(ctx context.Context, providerID string) (*Track, error)
	MediaSources(ctx context.Context, providerID string) ([]MediaSource, time.Time, error)
}

// TTLs per data type.
type TTLs struct {
	Search, Entity, Listing, Related, Browse, Lyrics time.Duration
}

var DefaultTTLs = TTLs{
	Search:  30 * time.Minute,
	Entity:  24 * time.Hour,
	Listing: time.Hour,
	Related: 10 * time.Minute,
	Browse:  30 * time.Minute,
	Lyrics:  24 * time.Hour,
}

// Service is the cached, provider-routing entry point for catalog reads.
type Service struct {
	primary     Provider
	sources     map[string]TrackSource
	cache       *cache.Cache
	ttl         TTLs
	loadTimeout time.Duration
	ytResolver  *innertube.Resolver
	ytMatchMu   sync.RWMutex
	ytMatched   map[string]string // trackID -> youtubeID

	// extraSongs adds songs from other sources (YouTube Music) to main
	// song search; deep searches the catalogs Deep Search adds.
	extraSongs SongSearch
	deep       SongSearch
}

// SongSearch finds songs for a query.
type SongSearch func(ctx context.Context, q string, limit int) ([]Track, error)

// extraSongsWait bounds how long main search waits for the extra source:
// past it, the primary's results go out alone.
const extraSongsWait = 3 * time.Second

// SetExtraSongSearch adds a second source to main song search.
func (s *Service) SetExtraSongSearch(fn SongSearch) { s.extraSongs = fn }

// SetDeepSongSearch sets the catalog search Deep Search adds to YouTube.
func (s *Service) SetDeepSongSearch(fn SongSearch) { s.deep = fn }

func NewService(primary Provider, c *cache.Cache, ttl TTLs) *Service {
	return &Service{
		primary:     primary,
		sources:     map[string]TrackSource{},
		cache:       c,
		ttl:         ttl,
		loadTimeout: 20 * time.Second,
		ytResolver:  innertube.NewResolver(),
		ytMatched:   make(map[string]string),
	}
}

// MatchedYouTubeID returns the matched YouTube video ID for a track if known.
func (s *Service) MatchedYouTubeID(trackID string) string {
	s.ytMatchMu.RLock()
	defer s.ytMatchMu.RUnlock()
	if s.ytMatched == nil {
		return ""
	}
	return s.ytMatched[trackID]
}

// SetMatchedYouTubeID records the matched YouTube video ID for a track.
func (s *Service) SetMatchedYouTubeID(trackID, ytID string) {
	s.ytMatchMu.Lock()
	defer s.ytMatchMu.Unlock()
	if s.ytMatched == nil {
		s.ytMatched = make(map[string]string)
	}
	s.ytMatched[trackID] = ytID
}

// MatchYouTube resolves a track to its matching YouTube video ID.
func (s *Service) MatchYouTube(ctx context.Context, t *Track) (string, error) {
	if t == nil {
		return "", fmt.Errorf("nil track")
	}
	if ytID := s.MatchedYouTubeID(t.ID); ytID != "" {
		return ytID, nil
	}

	// 1. Try zero-key InnerTube resolver first
	if s.ytResolver != nil {
		ytID, err := s.ytResolver.ResolveYouTubeID(ctx, t.Title, t.ArtistNames(), t.DurationSec)
		if err == nil && ytID != "" {
			s.SetMatchedYouTubeID(t.ID, ytID)
			return ytID, nil
		}
	}

	return "", fmt.Errorf("no youtube match found")
}

// RegisterSource adds a single-track source for a provider prefix.
func (s *Service) RegisterSource(provider string, src TrackSource) { s.sources[provider] = src }

func (s *Service) Cache() *cache.Cache { return s.cache }

// Remember stores tracks seen in other responses so later lookups by ID
// (queues, radio, Jam) can be served without an upstream call.
func (s *Service) Remember(tracks ...Track) {
	for i := range tracks {
		if tracks[i].ID == "" {
			continue
		}
		t := tracks[i]
		s.cache.Set("track:"+t.ID, &t, s.ttl.Entity)
	}
}

// NormalizeQuery trims, collapses whitespace and bounds a search query.
func NormalizeQuery(q string) (string, error) {
	q = strings.Join(strings.Fields(q), " ")
	if q == "" {
		return "", Invalid("query is required")
	}
	if len([]rune(q)) > 200 {
		return "", Invalid("query must be at most 200 characters")
	}
	for _, r := range q {
		if r < 0x20 || r == 0x7f {
			return "", Invalid("query contains control characters")
		}
	}
	return q, nil
}

func cacheKey(parts ...any) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprint(&b, p)
	}
	return b.String()
}

func load[T any](ctx context.Context, s *Service, ttl time.Duration, key string, fn func(context.Context) (T, error)) (T, error) {
	return cache.GetOrLoad(ctx, s.cache, key, s.loadTimeout, func(ctx context.Context) (T, time.Duration, error) {
		v, err := fn(ctx)
		return v, ttl, err
	})
}

func (s *Service) SearchSongs(ctx context.Context, q string, page, limit int) (*Page[Track], error) {
	k := cacheKey("search:songs", strings.ToLower(q), page, limit)
	p, err := load(ctx, s, s.ttl.Search, k, func(ctx context.Context) (*Page[Track], error) {
		if s.extraSongs == nil || page > 0 {
			return s.primary.SearchSongs(ctx, q, page, limit)
		}
		// Ask both sources at once; the extra one gets a short leash.
		type extra struct {
			tracks []Track
		}
		ch := make(chan extra, 1)
		go func() {
			ectx, cancel := context.WithTimeout(ctx, extraSongsWait)
			defer cancel()
			ts, _ := s.extraSongs(ectx, q, limit)
			ch <- extra{ts}
		}()
		p, err := s.primary.SearchSongs(ctx, q, page, limit)
		more := (<-ch).tracks
		if err != nil {
			if len(more) == 0 {
				return nil, err
			}
			p = &Page[Track]{Page: page, Limit: limit}
		}
		merged := interleave(p.Results, more, limit)
		out := *p
		out.Results = merged
		if out.Total < len(merged) {
			out.Total = len(merged)
		}
		return &out, nil
	})
	if err == nil {
		s.Remember(p.Results...)
	}
	return p, err
}

// interleave alternates the two lists (a first), dropping songs whose title
// and first artist already appeared, up to limit.
func interleave(a, b []Track, limit int) []Track {
	out := make([]Track, 0, min(limit, len(a)+len(b)))
	seen := map[string]bool{}
	add := func(t Track) {
		key := strings.ToLower(strings.TrimSpace(t.Title))
		if len(t.Artists) > 0 {
			key += "|" + strings.ToLower(strings.TrimSpace(t.Artists[0].Name))
		}
		if seen[key] || len(out) >= limit {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		if i < len(a) {
			add(a[i])
		}
		if i < len(b) {
			add(b[i])
		}
	}
	return out
}

func (s *Service) SearchAlbums(ctx context.Context, q string, page, limit int) (*Page[Album], error) {
	k := cacheKey("search:albums", strings.ToLower(q), page, limit)
	return load(ctx, s, s.ttl.Search, k, func(ctx context.Context) (*Page[Album], error) {
		return s.primary.SearchAlbums(ctx, q, page, limit)
	})
}

func (s *Service) SearchArtists(ctx context.Context, q string, page, limit int) (*Page[ArtistRef], error) {
	k := cacheKey("search:artists", strings.ToLower(q), page, limit)
	return load(ctx, s, s.ttl.Search, k, func(ctx context.Context) (*Page[ArtistRef], error) {
		return s.primary.SearchArtists(ctx, q, page, limit)
	})
}

func (s *Service) SearchPlaylists(ctx context.Context, q string, page, limit int) (*Page[Playlist], error) {
	k := cacheKey("search:playlists", strings.ToLower(q), page, limit)
	return load(ctx, s, s.ttl.Search, k, func(ctx context.Context) (*Page[Playlist], error) {
		return s.primary.SearchPlaylists(ctx, q, page, limit)
	})
}

func (s *Service) isPrimary(provider string) bool {
	if s.primary == nil {
		return false
	}
	if s.primary.Name() == provider {
		return true
	}
	if p, ok := s.primary.(interface{ Supports(string) bool }); ok && p.Supports(provider) {
		return true
	}
	return false
}

// Track returns one track by internal ID.
func (s *Service) Track(ctx context.Context, id string) (*Track, error) {
	provider, pid, ok := ParseID(id)
	if !ok {
		return nil, Invalid("invalid track id")
	}
	key := "track:" + MakeID(provider, pid)
	if s.isPrimary(provider) {
		return load(ctx, s, s.ttl.Entity, key, func(ctx context.Context) (*Track, error) {
			ts, err := s.primary.Tracks(ctx, []string{MakeID(provider, pid)})
			if err != nil {
				return nil, err
			}
			if len(ts) == 0 {
				return nil, NotFound("track not found")
			}
			return &ts[0], nil
		})
	}
	src, ok := s.sources[provider]
	if !ok {
		return nil, Unavailable("this track source is not enabled")
	}
	return load(ctx, s, s.ttl.Entity, key, func(ctx context.Context) (*Track, error) {
		return src.Track(ctx, pid)
	})
}

// Tracks returns tracks for internal IDs, preserving order and skipping
// tracks that could not be found. Cached tracks are served locally and the
// rest of the primary provider's IDs are fetched in one batch.
func (s *Service) Tracks(ctx context.Context, ids []string) ([]Track, error) {
	if len(ids) > 100 {
		return nil, Invalid("at most 100 ids per request")
	}
	found := make(map[string]Track, len(ids))
	var missing []string
	var others []string
	for _, id := range ids {
		provider, pid, ok := ParseID(id)
		if !ok {
			return nil, Invalid("invalid track id: " + truncate(id, 40))
		}
		full := MakeID(provider, pid)
		if v, ok := s.cache.Get("track:" + full); ok {
			found[full] = *v.(*Track)
			continue
		}
		if s.isPrimary(provider) {
			missing = append(missing, MakeID(provider, pid))
		} else {
			others = append(others, full)
		}
	}
	if len(missing) > 0 {
		ts, err := s.primary.Tracks(ctx, missing)
		if err != nil && KindOf(err) != KindNotFound {
			return nil, err
		}
		s.Remember(ts...)
		for _, t := range ts {
			found[t.ID] = t
		}
	}
	if len(others) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, id := range others {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if t, err := s.Track(ctx, id); err == nil {
					mu.Lock()
					found[t.ID] = *t
					mu.Unlock()
				}
			}(id)
		}
		wg.Wait()
	}
	out := make([]Track, 0, len(ids))
	for _, id := range ids {
		provider, pid, _ := ParseID(id)
		if t, ok := found[MakeID(provider, pid)]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Service) TrackByLink(ctx context.Context, token string) ([]Track, error) {
	ts, err := load(ctx, s, s.ttl.Entity, cacheKey("tracklink", token), func(ctx context.Context) ([]Track, error) {
		return s.primary.TrackByLink(ctx, token)
	})
	if err == nil {
		s.Remember(ts...)
	}
	return ts, err
}

// Related returns provider-related tracks for an internal track ID.
func (s *Service) Related(ctx context.Context, id string, limit int) ([]Track, error) {
	provider, pid, ok := ParseID(id)
	if !ok {
		return nil, Invalid("invalid track id")
	}
	if !s.isPrimary(provider) {
		return nil, Unavailable("related tracks are only available for catalog tracks")
	}
	ts, err := load(ctx, s, s.ttl.Related, cacheKey("related", MakeID(provider, pid), limit), func(ctx context.Context) ([]Track, error) {
		return s.primary.Related(ctx, MakeID(provider, pid), limit)
	})
	if err == nil {
		s.Remember(ts...)
	}
	return ts, err
}

func (s *Service) primaryID(id string) (string, error) {
	provider, pid, ok := ParseID(id)
	if !ok || !s.isPrimary(provider) {
		return "", Invalid("invalid id")
	}
	return MakeID(provider, pid), nil
}

func (s *Service) Album(ctx context.Context, id string) (*Album, error) {
	pid, err := s.primaryID(id)
	if err != nil {
		return nil, err
	}
	a, err := load(ctx, s, s.ttl.Entity, cacheKey("album", pid), func(ctx context.Context) (*Album, error) {
		return s.primary.Album(ctx, pid)
	})
	if err == nil {
		s.Remember(a.Tracks...)
	}
	return a, err
}

func (s *Service) AlbumByLink(ctx context.Context, token string) (*Album, error) {
	return load(ctx, s, s.ttl.Entity, cacheKey("albumlink", token), func(ctx context.Context) (*Album, error) {
		return s.primary.AlbumByLink(ctx, token)
	})
}

func (s *Service) Artist(ctx context.Context, id string, o ArtistOptions) (*Artist, error) {
	pid, err := s.primaryID(id)
	if err != nil {
		return nil, err
	}
	a, err := load(ctx, s, s.ttl.Entity, cacheKey("artist", pid, o), func(ctx context.Context) (*Artist, error) {
		return s.primary.Artist(ctx, pid, o)
	})
	if err == nil {
		s.Remember(a.TopSongs...)
		s.Remember(a.Singles...)
	}
	return a, err
}

func (s *Service) ArtistByLink(ctx context.Context, token string, o ArtistOptions) (*Artist, error) {
	return load(ctx, s, s.ttl.Entity, cacheKey("artistlink", token, o), func(ctx context.Context) (*Artist, error) {
		return s.primary.ArtistByLink(ctx, token, o)
	})
}

func (s *Service) ArtistSongs(ctx context.Context, id string, o ArtistOptions) (*Page[Track], error) {
	pid, err := s.primaryID(id)
	if err != nil {
		return nil, err
	}
	p, err := load(ctx, s, s.ttl.Listing, cacheKey("artistsongs", pid, o), func(ctx context.Context) (*Page[Track], error) {
		return s.primary.ArtistSongs(ctx, pid, o)
	})
	if err == nil {
		s.Remember(p.Results...)
	}
	return p, err
}

func (s *Service) ArtistAlbums(ctx context.Context, id string, o ArtistOptions) (*Page[Album], error) {
	pid, err := s.primaryID(id)
	if err != nil {
		return nil, err
	}
	return load(ctx, s, s.ttl.Listing, cacheKey("artistalbums", pid, o), func(ctx context.Context) (*Page[Album], error) {
		return s.primary.ArtistAlbums(ctx, pid, o)
	})
}

func (s *Service) Playlist(ctx context.Context, id string, page, limit int) (*Playlist, error) {
	pid, err := s.primaryID(id)
	if err != nil {
		return nil, err
	}
	p, err := load(ctx, s, s.ttl.Entity, cacheKey("playlist", pid, page, limit), func(ctx context.Context) (*Playlist, error) {
		return s.primary.Playlist(ctx, pid, page, limit)
	})
	if err == nil {
		s.Remember(p.Tracks...)
	}
	return p, err
}

func (s *Service) PlaylistByLink(ctx context.Context, token string, page, limit int) (*Playlist, error) {
	return load(ctx, s, s.ttl.Entity, cacheKey("playlistlink", token, page, limit), func(ctx context.Context) (*Playlist, error) {
		return s.primary.PlaylistByLink(ctx, token, page, limit)
	})
}

func (s *Service) Lyrics(ctx context.Context, id string) (*Lyrics, error) {
	pid, err := s.primaryID(id)
	if err != nil {
		return nil, err
	}
	return load(ctx, s, s.ttl.Lyrics, cacheKey("lyrics", pid), func(ctx context.Context) (*Lyrics, error) {
		return s.primary.Lyrics(ctx, pid)
	})
}

func (s *Service) Trending(ctx context.Context, language string) ([]Collection, error) {
	return load(ctx, s, s.ttl.Browse, cacheKey("trending", language), func(ctx context.Context) ([]Collection, error) {
		return s.primary.Trending(ctx, language)
	})
}

func (s *Service) Browse(ctx context.Context, language string) ([]Collection, error) {
	return load(ctx, s, s.ttl.Browse, cacheKey("browse", language), func(ctx context.Context) ([]Collection, error) {
		return s.primary.Browse(ctx, language)
	})
}

// PreviewURL is a catalog song's own short preview (Deezer, iTunes,
// Spotify), or "" when it has none. It never touches YouTube.
func (s *Service) PreviewURL(ctx context.Context, t *Track) string {
	if t == nil {
		return ""
	}
	srcs, _, err := s.MediaSources(ctx, t.ID)
	if err != nil {
		return ""
	}
	for _, src := range srcs {
		if src.Kind == "audio" && src.URL != "" {
			return src.URL
		}
	}
	return ""
}

// SearchSongsDeep searches the catalogs Deep Search adds next to YouTube
// (Deezer, iTunes, MusicBrainz, Spotify).
func (s *Service) SearchSongsDeep(ctx context.Context, q string, limit int) ([]Track, error) {
	if s.deep == nil {
		return nil, nil
	}
	ts, err := load(ctx, s, s.ttl.Search, cacheKey("search:deep", strings.ToLower(q), limit), func(ctx context.Context) ([]Track, error) {
		return s.deep(ctx, q, limit)
	})
	if err != nil {
		return nil, err
	}
	s.Remember(ts...)
	return ts, nil
}

// MediaSources is not cached here: the playback package owns descriptor
// caching because it knows about expiry.
func (s *Service) MediaSources(ctx context.Context, id string) ([]MediaSource, time.Time, error) {
	provider, pid, ok := ParseID(id)
	if !ok {
		return nil, time.Time{}, Invalid("invalid track id")
	}
	if s.isPrimary(provider) {
		return s.primary.MediaSources(ctx, MakeID(provider, pid))
	}
	if src, ok := s.sources[provider]; ok {
		return src.MediaSources(ctx, pid)
	}
	return nil, time.Time{}, Unavailable("this track source is not enabled")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
