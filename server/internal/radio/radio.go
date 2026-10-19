// Package radio generates continuous listening sessions.
//
// Generation is transparent: each session reports how its tracks are
// produced. Song radio uses Saavn's own song station (a verified provider
// capability) re-ranked by the recommendation model; every other kind is a
// Swar-generated queue built from catalog metadata and listening signals
// and is labelled as such, never as an official provider station.
//
// Sessions live in memory (lost on restart, documented) and only hand out
// track metadata. Radio never downloads anything.
package radio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
	"swar/internal/recommendations"
)

type Kind string

const (
	KindSong         Kind = "song"
	KindArtist       Kind = "artist"
	KindAlbum        Kind = "album"
	KindPersonalized Kind = "personalized"
	KindGenre        Kind = "genre"
	KindDiscovery    Kind = "discovery"
)

// Generation labels how tracks are produced.
const (
	GenProviderStation = "provider_station_ranked" // Saavn station + Swar ranking
	GenRecommendation  = "swar_recommendations"
	GenSearch          = "catalog_search"
	GenDiscovery       = "random_discovery"
)

const (
	batchDefault   = 10
	batchMax       = 25
	maxServed      = 500
	refillBelow    = 8
	maxPerOwner    = 3
	maxRefillTries = 3
)

var genreRe = regexp.MustCompile(`^[\p{L}\p{N} &'\-]{2,40}$`)

// discoverySeeds are broad, safe search terms used for Random Discovery.
var discoverySeeds = []string{
	"bollywood hits", "indie india", "punjabi pop", "tamil melodies", "telugu hits",
	"lofi", "sufi", "ghazal", "classical instrumental", "retro hindi", "malayalam hits",
	"marathi songs", "bengali songs", "indian rock", "devotional", "acoustic covers",
}

type Session struct {
	ID              string    `json:"id"`
	Kind            Kind      `json:"kind"`
	Seed            string    `json:"seed"`
	Label           string    `json:"label"`
	Generation      string    `json:"generation"`
	ProviderStation bool      `json:"providerStation"`
	CreatedAt       time.Time `json:"createdAt"`

	owner     string
	mu        sync.Mutex
	lastUsed  time.Time
	served    []catalog.Track
	pending   []catalog.Track
	seen      map[string]bool
	liked     map[string]bool
	disliked  map[string]bool
	skipped   map[string]int
	page      int // for search/artist-based refills
	exhausted bool
}

type Snapshot struct {
	ID              string          `json:"id"`
	Kind            Kind            `json:"kind"`
	Seed            string          `json:"seed"`
	Label           string          `json:"label"`
	Generation      string          `json:"generation"`
	ProviderStation bool            `json:"providerStation"`
	Tracks          []catalog.Track `json:"tracks"`
	ServedCount     int             `json:"servedCount"`
	Exhausted       bool            `json:"exhausted"`
}

type Manager struct {
	cat     *catalog.Service
	rec     *recommendations.Service
	signals recommendations.SignalSource
	ttl     time.Duration
	now     func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager(cat *catalog.Service, rec *recommendations.Service, sig recommendations.SignalSource, ttl time.Duration) *Manager {
	return &Manager{cat: cat, rec: rec, signals: sig, ttl: ttl, now: time.Now, sessions: map[string]*Session{}}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "rad_" + hex.EncodeToString(b)
}

// Start creates a session and returns its first batch.
func (m *Manager) Start(ctx context.Context, owner string, kind Kind, seed string, count int) (*Snapshot, error) {
	s := &Session{
		ID: newID(), Kind: kind, Seed: strings.TrimSpace(seed), CreatedAt: m.now(), owner: owner,
		lastUsed: m.now(), seen: map[string]bool{}, liked: map[string]bool{}, disliked: map[string]bool{}, skipped: map[string]int{},
	}
	if err := m.initSession(ctx, s); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.gcLocked()
	var mine []*Session
	for _, o := range m.sessions {
		if o.owner == owner {
			mine = append(mine, o)
		}
	}
	// Bound sessions per owner: drop the least recently used.
	for len(mine) >= maxPerOwner {
		oldest := 0
		for i, o := range mine {
			if o.lastUsed.Before(mine[oldest].lastUsed) {
				oldest = i
			}
		}
		delete(m.sessions, mine[oldest].ID)
		mine = append(mine[:oldest], mine[oldest+1:]...)
	}
	m.sessions[s.ID] = s
	m.mu.Unlock()

	return m.next(ctx, s, count)
}

func (m *Manager) initSession(ctx context.Context, s *Session) error {
	switch s.Kind {
	case KindSong:
		t, err := m.cat.Track(ctx, s.Seed)
		if err != nil {
			return err
		}
		s.Seed = t.ID
		s.Label = "Song radio: " + t.Title
		if t.Provider == catalog.ProviderSaavn {
			s.Generation, s.ProviderStation = GenProviderStation, true
		} else {
			s.Generation = GenRecommendation
		}
		s.seen[t.ID] = true
		s.served = append(s.served, *t)
		return m.fillFromSeed(ctx, s, t.ID)
	case KindArtist:
		a, err := m.cat.Artist(ctx, s.Seed, catalog.ArtistOptions{SongCount: 25, AlbumCount: 0, SortBy: catalog.SortPopularity, SortOrder: "desc"})
		if err != nil {
			return err
		}
		s.Seed, s.Label, s.Generation = a.ID, "Artist radio: "+a.Name, GenRecommendation
		pool := append([]catalog.Track{}, a.TopSongs...)
		for i, sim := range a.SimilarArtists {
			if i >= 3 {
				break
			}
			if p, err := m.cat.ArtistSongs(ctx, sim.ID, catalog.ArtistOptions{SortBy: catalog.SortPopularity, SortOrder: "desc"}); err == nil {
				pool = append(pool, firstN(p.Results, 5)...)
			}
		}
		m.addPending(s, interleaveByArtist(pool))
	case KindAlbum:
		a, err := m.cat.Album(ctx, s.Seed)
		if err != nil {
			return err
		}
		s.Seed, s.Label, s.Generation = a.ID, "Album radio: "+a.Name, GenRecommendation
		m.addPending(s, a.Tracks)
		if len(a.Tracks) > 0 {
			_ = m.fillFromSeed(ctx, s, a.Tracks[0].ID)
		}
	case KindPersonalized:
		s.Label, s.Generation = "Your radio", GenRecommendation
		seeds := m.personalSeeds(ctx, s.owner)
		for _, id := range seeds {
			_ = m.fillFromSeed(ctx, s, id)
			if len(s.pending) >= 20 {
				break
			}
		}
		if len(s.pending) == 0 {
			// No history yet: be honest and fall back to discovery.
			s.Kind, s.Label, s.Generation = KindDiscovery, "Discovery radio (not enough listening history yet)", GenDiscovery
			return m.fillFromSearch(ctx, s, discoverySeeds[mrand.IntN(len(discoverySeeds))])
		}
	case KindGenre:
		g := strings.Join(strings.Fields(s.Seed), " ")
		if !genreRe.MatchString(g) {
			return catalog.Invalid("genre must be 2-40 letters, numbers or spaces")
		}
		s.Seed, s.Label, s.Generation = g, "Genre radio: "+g, GenSearch
		return m.fillFromSearch(ctx, s, g)
	case KindDiscovery:
		s.Seed = discoverySeeds[mrand.IntN(len(discoverySeeds))]
		s.Label, s.Generation = "Random discovery", GenDiscovery
		return m.fillFromSearch(ctx, s, s.Seed)
	default:
		return catalog.Invalid("unknown radio kind")
	}
	if len(s.pending) == 0 && len(s.served) == 0 {
		return catalog.NotFound("could not find tracks for this radio")
	}
	return nil
}

func (m *Manager) personalSeeds(ctx context.Context, owner string) []string {
	if owner == "" || m.signals == nil {
		return nil
	}
	sig, err := m.signals.Signals(ctx, owner)
	if err != nil {
		return nil
	}
	var liked, recent []string
	for id := range sig.Liked {
		liked = append(liked, id)
	}
	for id := range sig.RecentPlays {
		if !sig.Disliked[id] && sig.Skips[id] == 0 {
			recent = append(recent, id)
		}
	}
	mrand.Shuffle(len(liked), func(i, j int) { liked[i], liked[j] = liked[j], liked[i] })
	mrand.Shuffle(len(recent), func(i, j int) { recent[i], recent[j] = recent[j], recent[i] })
	return append(firstNStr(liked, 3), firstNStr(recent, 2)...)
}

func (m *Manager) fillFromSeed(ctx context.Context, s *Session, seedID string) error {
	exclude := make([]string, 0, len(s.seen))
	for id := range s.seen {
		exclude = append(exclude, id)
	}
	resp, err := m.rec.Next(ctx, recommendations.Request{SeedTrackID: seedID, Exclude: exclude, Limit: 20, Device: s.owner})
	if err != nil {
		return err
	}
	ts := make([]catalog.Track, 0, len(resp.Results))
	for _, r := range resp.Results {
		ts = append(ts, r.Track)
	}
	m.addPending(s, ts)
	return nil
}

func (m *Manager) fillFromSearch(ctx context.Context, s *Session, q string) error {
	s.page++
	p, err := m.cat.SearchSongs(ctx, q, s.page, 20)
	if err != nil {
		return err
	}
	ts := append([]catalog.Track{}, p.Results...)
	mrand.Shuffle(len(ts), func(i, j int) { ts[i], ts[j] = ts[j], ts[i] })
	m.addPending(s, ts)
	return nil
}

func (m *Manager) addPending(s *Session, ts []catalog.Track) {
	for _, t := range ts {
		if t.ID == "" || s.seen[t.ID] || s.disliked[t.ID] {
			continue
		}
		s.seen[t.ID] = true
		s.pending = append(s.pending, t)
	}
}

// Next serves the next batch, refilling from the latest good seed.
func (m *Manager) Next(ctx context.Context, owner, id string, count int) (*Snapshot, error) {
	s, err := m.get(owner, id)
	if err != nil {
		return nil, err
	}
	return m.next(ctx, s, count)
}

func (m *Manager) next(ctx context.Context, s *Session, count int) (*Snapshot, error) {
	if count < 1 {
		count = batchDefault
	}
	count = min(count, batchMax)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUsed = m.now()

	for tries := 0; len(s.pending) < max(count, refillBelow) && !s.exhausted && tries < maxRefillTries; tries++ {
		before := len(s.pending)
		m.refill(ctx, s)
		if len(s.pending) == before && tries == maxRefillTries-1 {
			s.exhausted = len(s.pending) == 0
		}
	}
	batch := []catalog.Track{}
	for len(batch) < count && len(s.pending) > 0 && len(s.served) < maxServed {
		t := s.pending[0]
		s.pending = s.pending[1:]
		batch = append(batch, t)
		s.served = append(s.served, t)
	}
	if len(s.served) >= maxServed {
		s.exhausted = true
	}
	m.cat.Remember(batch...)
	return s.snapshot(batch), nil
}

func (m *Manager) refill(ctx context.Context, s *Session) {
	switch s.Kind {
	case KindGenre, KindDiscovery:
		q := s.Seed
		if s.Kind == KindDiscovery && s.page >= 3 {
			// Wander to a new area after a few pages.
			q = discoverySeeds[mrand.IntN(len(discoverySeeds))]
			s.Seed, s.page = q, 0
		}
		_ = m.fillFromSearch(ctx, s, q)
	default:
		if seed := s.bestSeed(); seed != "" {
			_ = m.fillFromSeed(ctx, s, seed)
		}
	}
}

// bestSeed prefers the most recent liked track, then the most recent
// served track that was not skipped or disliked.
func (s *Session) bestSeed() string {
	for i := len(s.served) - 1; i >= 0; i-- {
		if s.liked[s.served[i].ID] {
			return s.served[i].ID
		}
	}
	for i := len(s.served) - 1; i >= 0; i-- {
		id := s.served[i].ID
		if !s.disliked[id] && s.skipped[id] == 0 {
			return id
		}
	}
	if len(s.served) > 0 {
		return s.served[0].ID
	}
	return ""
}

// Feedback records like/dislike/skip and adapts the pending queue.
func (m *Manager) Feedback(owner, id, trackID, action string) (*Snapshot, error) {
	s, err := m.get(owner, id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "like":
		s.liked[trackID] = true
		delete(s.disliked, trackID)
	case "dislike":
		s.disliked[trackID] = true
		delete(s.liked, trackID)
		// Drop pending tracks by the same primary artist.
		artist := ""
		for _, t := range s.served {
			if t.ID == trackID && len(t.Artists) > 0 {
				artist = t.Artists[0].Name
			}
		}
		if artist != "" {
			kept := s.pending[:0]
			for _, t := range s.pending {
				if len(t.Artists) == 0 || t.Artists[0].Name != artist {
					kept = append(kept, t)
				}
			}
			s.pending = kept
		}
	case "skip":
		s.skipped[trackID]++
	default:
		return nil, catalog.Invalid("action must be like, dislike or skip")
	}
	return s.snapshot(nil), nil
}

func (m *Manager) Get(owner, id string) (*Snapshot, error) {
	s, err := m.get(owner, id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(nil), nil
}

func (m *Manager) Stop(owner, id string) error {
	if _, err := m.get(owner, id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return nil
}

func (m *Manager) get(owner, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.owner != owner || m.now().Sub(s.lastUsed) > m.ttl {
		return nil, catalog.NotFound("radio session not found or expired")
	}
	return s, nil
}

// GC removes expired sessions; call periodically.
func (m *Manager) GC() {
	m.mu.Lock()
	m.gcLocked()
	m.mu.Unlock()
}

func (m *Manager) gcLocked() {
	for id, s := range m.sessions {
		if m.now().Sub(s.lastUsed) > m.ttl {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (s *Session) snapshot(batch []catalog.Track) *Snapshot {
	if batch == nil {
		batch = []catalog.Track{}
	}
	return &Snapshot{
		ID: s.ID, Kind: s.Kind, Seed: s.Seed, Label: s.Label, Generation: s.Generation,
		ProviderStation: s.ProviderStation, Tracks: batch, ServedCount: len(s.served), Exhausted: s.exhausted && len(s.pending) == 0,
	}
}

// interleaveByArtist spreads tracks so one artist does not dominate.
func interleaveByArtist(ts []catalog.Track) []catalog.Track {
	groups := map[string][]catalog.Track{}
	order := []string{}
	for _, t := range ts {
		k := ""
		if len(t.Artists) > 0 {
			k = t.Artists[0].Name
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], t)
	}
	out := make([]catalog.Track, 0, len(ts))
	for len(out) < len(ts) {
		for _, k := range order {
			if g := groups[k]; len(g) > 0 {
				out = append(out, g[0])
				groups[k] = g[1:]
			}
		}
	}
	return out
}

func firstN(ts []catalog.Track, n int) []catalog.Track {
	if len(ts) > n {
		return ts[:n]
	}
	return ts
}

func firstNStr(ts []string, n int) []string {
	if len(ts) > n {
		return ts[:n]
	}
	return ts
}
