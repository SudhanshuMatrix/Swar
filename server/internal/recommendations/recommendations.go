// Package recommendations ranks candidate next tracks with a small,
// explainable scoring model. No paid AI API is involved: every score is a
// sum of named signals, and each signal that fired is returned as a reason
// code so the client (and a curious user) can see why a track was picked.
package recommendations

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
)

// Signals summarize what we know about the listener. All fields are
// optional; anonymous requests simply have empty signals.
type Signals struct {
	Liked         map[string]bool
	Disliked      map[string]bool
	LikedArtists  map[string]int // artist ID -> liked track count
	RecentPlays   map[string]time.Time
	Skips         map[string]int
	Languages     map[string]bool // preferred languages (lower-case)
	BlockExplicit bool
}

func (s *Signals) init() {
	if s.Liked == nil {
		s.Liked = map[string]bool{}
	}
	if s.Disliked == nil {
		s.Disliked = map[string]bool{}
	}
	if s.LikedArtists == nil {
		s.LikedArtists = map[string]int{}
	}
	if s.RecentPlays == nil {
		s.RecentPlays = map[string]time.Time{}
	}
	if s.Skips == nil {
		s.Skips = map[string]int{}
	}
	if s.Languages == nil {
		s.Languages = map[string]bool{}
	}
}

// Reason codes.
const (
	ReasonProviderRelated = "provider_related"
	ReasonSameArtist      = "same_artist"
	ReasonSameAlbum       = "same_album"
	ReasonLikedArtist     = "liked_artist"
	ReasonLiked           = "liked_before"
	ReasonLanguage        = "preferred_language"
	ReasonSameLanguage    = "same_language_as_seed"
	ReasonPlaylistContext = "playlist_context"
	ReasonRecentPenalty   = "recently_played"
	ReasonSkipPenalty     = "often_skipped"
	ReasonPopular         = "popular"
)

// Weights are exported so they are easy to tune and to read in tests.
var Weights = struct {
	ProviderRelated, SameArtist, SameAlbum, LikedArtist, Liked, Language,
	SameLanguage, PlaylistContext, RecentPenalty, SkipPenalty, Popular float64
}{
	ProviderRelated: 3, SameArtist: 2, SameAlbum: 1, LikedArtist: 1.5, Liked: 1,
	Language: 1, SameLanguage: 0.75, PlaylistContext: 1.5, RecentPenalty: -4,
	SkipPenalty: -1.5, Popular: 0.5,
}

// Candidate is a track plus where it came from.
type Candidate struct {
	Track   catalog.Track
	Sources []string // e.g. provider_related, same_artist, playlist_context
	Rank    int      // position within its source (lower is better)
}

type Ranked struct {
	Track   catalog.Track `json:"track"`
	Score   float64       `json:"score"`
	Reasons []string      `json:"reasons"`
}

// Rank scores and orders candidates. Tracks that are disliked, explicit
// when blocked, the seed itself, or in exclude are dropped. Ties keep the
// provider's own order so results are deterministic.
func Rank(seed *catalog.Track, cands []Candidate, sig Signals, exclude map[string]bool, now time.Time) []Ranked {
	sig.init()
	seedArtists := map[string]bool{}
	seedAlbum, seedLang := "", ""
	if seed != nil {
		for _, id := range seed.ArtistIDs() {
			seedArtists[id] = true
		}
		if seed.Album != nil {
			seedAlbum = seed.Album.ID
		}
		seedLang = strings.ToLower(seed.Language)
	}

	merged := map[string]*Candidate{}
	order := []string{}
	for _, c := range cands {
		id := c.Track.ID
		if id == "" {
			continue
		}
		if m, ok := merged[id]; ok {
			m.Sources = append(m.Sources, c.Sources...)
			m.Rank = min(m.Rank, c.Rank)
			continue
		}
		cc := c
		cc.Sources = append([]string(nil), c.Sources...)
		merged[id] = &cc
		order = append(order, id)
	}

	out := make([]Ranked, 0, len(merged))
	for _, id := range order {
		c := merged[id]
		t := c.Track
		if (seed != nil && t.ID == seed.ID) || exclude[t.ID] || sig.Disliked[t.ID] {
			continue
		}
		if sig.BlockExplicit && t.Explicit {
			continue
		}
		var score float64
		reasons := []string{}
		add := func(w float64, reason string) {
			score += w
			reasons = append(reasons, reason)
		}
		has := func(src string) bool {
			for _, s := range c.Sources {
				if s == src {
					return true
				}
			}
			return false
		}
		if has(ReasonProviderRelated) {
			// Earlier provider suggestions are slightly stronger.
			add(Weights.ProviderRelated-0.05*float64(min(c.Rank, 20)), ReasonProviderRelated)
		}
		if has(ReasonPlaylistContext) {
			add(Weights.PlaylistContext, ReasonPlaylistContext)
		}
		for _, aid := range t.ArtistIDs() {
			if seedArtists[aid] {
				add(Weights.SameArtist, ReasonSameArtist)
				break
			}
		}
		if seedAlbum != "" && t.Album != nil && t.Album.ID == seedAlbum {
			add(Weights.SameAlbum, ReasonSameAlbum)
		}
		for _, aid := range t.ArtistIDs() {
			if sig.LikedArtists[aid] > 0 {
				add(Weights.LikedArtist, ReasonLikedArtist)
				break
			}
		}
		if sig.Liked[t.ID] {
			add(Weights.Liked, ReasonLiked)
		}
		lang := strings.ToLower(t.Language)
		if lang != "" && sig.Languages[lang] {
			add(Weights.Language, ReasonLanguage)
		} else if lang != "" && lang == seedLang {
			add(Weights.SameLanguage, ReasonSameLanguage)
		}
		if t.PlayCount > 10_000_000 {
			add(Weights.Popular, ReasonPopular)
		}
		if at, ok := sig.RecentPlays[t.ID]; ok {
			// Full penalty for the last 2 hours, fading out over a week.
			age := now.Sub(at)
			factor := 1.0
			if age > 2*time.Hour {
				factor = max(0, 1-float64(age-2*time.Hour)/float64(7*24*time.Hour))
			}
			if factor > 0 {
				add(Weights.RecentPenalty*factor, ReasonRecentPenalty)
			}
		}
		if n := sig.Skips[t.ID]; n > 0 {
			add(Weights.SkipPenalty*float64(min(n, 3)), ReasonSkipPenalty)
		}
		out = append(out, Ranked{Track: t, Score: round2(score), Reasons: reasons})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// ApplyArtistDiversity reorders so the same primary artist does not appear
// more than maxRun times in a row, without dropping tracks.
func ApplyArtistDiversity(in []Ranked, maxRun int) []Ranked {
	if maxRun < 1 || len(in) < 3 {
		return in
	}
	out := make([]Ranked, 0, len(in))
	pending := append([]Ranked(nil), in...)
	for len(pending) > 0 {
		picked := 0
		for i, r := range pending {
			if !wouldRepeat(out, r, maxRun) {
				picked = i
				break
			}
		}
		out = append(out, pending[picked])
		pending = append(pending[:picked], pending[picked+1:]...)
	}
	return out
}

func wouldRepeat(out []Ranked, r Ranked, maxRun int) bool {
	if len(out) < maxRun {
		return false
	}
	a := primaryArtist(r.Track)
	if a == "" {
		return false
	}
	for _, prev := range out[len(out)-maxRun:] {
		if primaryArtist(prev.Track) != a {
			return false
		}
	}
	return true
}

func primaryArtist(t catalog.Track) string {
	if len(t.Artists) == 0 {
		return ""
	}
	if t.Artists[0].ID != "" {
		return t.Artists[0].ID
	}
	return strings.ToLower(t.Artists[0].Name)
}

func round2(f float64) float64 {
	if f < 0 {
		return -round2(-f)
	}
	return float64(int64(f*100+0.5)) / 100
}

// SignalSource loads signals for an authenticated device.
type SignalSource interface {
	Signals(ctx context.Context, device string) (Signals, error)
}

// Service gathers candidates from the catalog and ranks them.
type Service struct {
	cat     *catalog.Service
	signals SignalSource
	now     func() time.Time
}

func NewService(cat *catalog.Service, signals SignalSource) *Service {
	return &Service{cat: cat, signals: signals, now: time.Now}
}

type Request struct {
	SeedTrackID     string
	Exclude         []string // e.g. tracks already in the queue
	PlaylistContext []string // track IDs from the playlist being played
	Limit           int
	Device          string // optional; enables personal signals
}

type Response struct {
	Seed    *catalog.Track `json:"seed,omitempty"`
	Results []Ranked       `json:"results"`
	// Sources lists the candidate pools that answered, for transparency.
	Sources  []string `json:"sources"`
	Degraded bool     `json:"degraded"` // some candidate pools failed
}

// Next returns ranked next-track candidates for a seed track. It is best
// effort: a failing pool degrades the result instead of failing it, and an
// empty result is a valid answer.
func (s *Service) Next(ctx context.Context, req Request) (*Response, error) {
	if req.Limit < 1 || req.Limit > 50 {
		req.Limit = 10
	}
	seed, err := s.cat.Track(ctx, req.SeedTrackID)
	if err != nil {
		return nil, err
	}
	var sig Signals
	if req.Device != "" && s.signals != nil {
		if sg, err := s.signals.Signals(ctx, req.Device); err == nil {
			sig = sg
		}
	}

	resp := &Response{Seed: seed, Sources: []string{}}
	var mu sync.Mutex
	var cands []Candidate
	var wg sync.WaitGroup
	pool := func(name string, fn func() ([]catalog.Track, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts, err := fn()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				resp.Degraded = true
				return
			}
			resp.Sources = append(resp.Sources, name)
			for i, t := range ts {
				cands = append(cands, Candidate{Track: t, Sources: []string{name}, Rank: i})
			}
		}()
	}
	if seed.Provider == catalog.ProviderSaavn {
		pool(ReasonProviderRelated, func() ([]catalog.Track, error) { return s.cat.Related(ctx, seed.ID, 20) })
		if len(seed.Artists) > 0 && seed.Artists[0].ID != "" {
			artistID := seed.Artists[0].ID
			pool(ReasonSameArtist, func() ([]catalog.Track, error) {
				p, err := s.cat.ArtistSongs(ctx, artistID, catalog.ArtistOptions{SortBy: catalog.SortPopularity, SortOrder: "desc"})
				if err != nil {
					return nil, err
				}
				return p.Results, nil
			})
		}
	} else if name := seed.ArtistNames(); name != "" {
		// Non-catalog seeds: look the artist up in the catalog by name.
		pool(ReasonSameArtist, func() ([]catalog.Track, error) {
			p, err := s.cat.SearchSongs(ctx, name, 1, 20)
			if err != nil {
				return nil, err
			}
			return p.Results, nil
		})
	}
	if len(req.PlaylistContext) > 0 {
		ids := req.PlaylistContext
		if len(ids) > 50 {
			ids = ids[:50]
		}
		pool(ReasonPlaylistContext, func() ([]catalog.Track, error) { return s.cat.Tracks(ctx, ids) })
	}
	wg.Wait()
	sort.Strings(resp.Sources)

	exclude := map[string]bool{}
	for _, id := range req.Exclude {
		exclude[id] = true
	}
	ranked := Rank(seed, cands, sig, exclude, s.now())
	ranked = ApplyArtistDiversity(ranked, 2)
	if len(ranked) > req.Limit {
		ranked = ranked[:req.Limit]
	}
	resp.Results = ranked
	return resp, nil
}
