// Package recommendations ranks candidate next tracks with a small,
// explainable scoring model. No paid AI API is involved: every score is a
// sum of named signals, and each signal that fired is returned as a reason
// code so the client (and a curious user) can see why a track was picked.
package recommendations

import (
	"context"
	"regexp"
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
	ReasonSessionRelated  = "session_related"
	ReasonSessionArtist   = "session_artist"
	ReasonSessionSkipped  = "skipped_this_session"
	ReasonSameEra         = "same_era"
	ReasonDifferentEra    = "different_era"
	ReasonTooShort        = "too_short"
)

// Weights are exported so they are easy to tune and to read in tests.
var Weights = struct {
	ProviderRelated, SameArtist, SameAlbum, LikedArtist, Liked, Language,
	SameLanguage, PlaylistContext, RecentPenalty, SkipPenalty, Popular,
	SessionRelated, SessionArtist, SessionSkipped, SameEra, DifferentEra, TooShort float64
}{
	ProviderRelated: 3, SameArtist: 2, SameAlbum: 1, LikedArtist: 1.5, Liked: 1,
	Language: 1, SameLanguage: 0.75, PlaylistContext: 1.5, RecentPenalty: -4,
	SkipPenalty: -1.5, Popular: 0.5,
	SessionRelated: 1.5, SessionArtist: 1, SessionSkipped: -2.5, SameEra: 0.5, DifferentEra: -0.5, TooShort: -2,
}

// Session is what happened earlier in the current listening session. It
// lets recommendations follow the mood of the last few songs instead of
// only the one playing now, and back off from what the listener skipped.
type Session struct {
	Recent  []catalog.Track // played before the seed, newest first
	Skipped map[string]bool // track IDs skipped in this session
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
	return RankSession(seed, Session{}, cands, sig, exclude, now)
}

// RankSession is Rank with listening-session context:
//   - artists from recent songs get a boost that fades with each older song,
//     and artists whose songs were skipped this session are pushed down;
//   - songs from the same era as the seed get a small boost;
//   - very short tracks (intros, skits) are pushed down;
//   - other versions of a song already heard (remix, lofi, reprise...) are
//     dropped, and only the best version of any song is kept.
func RankSession(seed *catalog.Track, sess Session, cands []Candidate, sig Signals, exclude map[string]bool, now time.Time) []Ranked {
	sig.init()
	heardTitles := map[string]bool{}
	if seed != nil {
		if k := titleKey(seed.Title); k != "" {
			heardTitles[k] = true
		}
	}
	artistAffinity := map[string]float64{}
	weight := 1.0
	for _, t := range sess.Recent {
		weight *= 0.6
		if k := titleKey(t.Title); k != "" {
			heardTitles[k] = true
		}
		delta := weight
		if sess.Skipped[t.ID] {
			delta = -1
		}
		for _, a := range t.ArtistIDs() {
			artistAffinity[a] += delta
		}
	}
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
		if k := titleKey(t.Title); k != "" && heardTitles[k] {
			continue // another version of a song already heard
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
		if has(ReasonSessionRelated) && !has(ReasonProviderRelated) {
			add(Weights.SessionRelated-0.05*float64(min(c.Rank, 20)), ReasonSessionRelated)
		}
		affinity := 0.0
		for _, aid := range t.ArtistIDs() {
			affinity = max(affinity, artistAffinity[aid])
			if artistAffinity[aid] < 0 {
				affinity = min(affinity, artistAffinity[aid])
				break
			}
		}
		if affinity > 0.05 {
			add(Weights.SessionArtist*min(affinity, 1.5), ReasonSessionArtist)
		} else if affinity < 0 {
			add(Weights.SessionSkipped, ReasonSessionSkipped)
		}
		if sess.Skipped[t.ID] {
			add(Weights.SessionSkipped, ReasonSessionSkipped)
		}
		if seed != nil && seed.Year > 0 && t.Year > 0 {
			switch dy := abs(seed.Year - t.Year); {
			case dy <= 3:
				add(Weights.SameEra, ReasonSameEra)
			case dy >= 15:
				add(Weights.DifferentEra, ReasonDifferentEra)
			}
		}
		if t.DurationSec > 0 && t.DurationSec < 60 {
			add(Weights.TooShort, ReasonTooShort)
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

	// Keep only the best-scoring version of each song.
	seen := map[string]bool{}
	deduped := out[:0]
	for _, r := range out {
		if k := titleKey(r.Track.Title); k != "" {
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		deduped = append(deduped, r)
	}
	return deduped
}

// Diversify re-orders ranked tracks with maximal marginal relevance: each
// pick is the track with the best score after a penalty for resembling the
// last few picks (same artist, same album). Strong matches still come
// first, but the list mixes artists instead of clustering them.
func Diversify(in []Ranked, penalty float64) []Ranked {
	if len(in) < 3 || penalty <= 0 {
		return in
	}
	pending := append([]Ranked(nil), in...)
	out := make([]Ranked, 0, len(in))
	for len(pending) > 0 {
		best, bestVal := 0, 0.0
		for i, r := range pending {
			v := r.Score - penalty*similarityToRecent(out, r, 3)
			if i == 0 || v > bestVal {
				best, bestVal = i, v
			}
		}
		out = append(out, pending[best])
		pending = append(pending[:best], pending[best+1:]...)
	}
	return out
}

func similarityToRecent(picked []Ranked, r Ranked, window int) float64 {
	sim := 0.0
	start := max(0, len(picked)-window)
	for _, p := range picked[start:] {
		s := 0.0
		if a := primaryArtist(r.Track); a != "" && a == primaryArtist(p.Track) {
			s += 1
		}
		if r.Track.Album != nil && p.Track.Album != nil && r.Track.Album.ID != "" && r.Track.Album.ID == p.Track.Album.ID {
			s += 0.5
		}
		sim = max(sim, s)
	}
	return sim
}

var (
	bracketed    = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)
	versionWords = regexp.MustCompile(`\b(remix|remixed|lofi|lo-fi|slowed|reverb|reprise|unplugged|acoustic|version|edit|mix|cover|live|female|male|sped up|8d|instrumental|karaoke)\b`)
	nonWord      = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// titleKey normalizes a title so versions of one song compare equal:
// "Kesariya (From \"Brahmastra\") - Lofi Mix" -> "kesariya".
func titleKey(title string) string {
	t := strings.ToLower(title)
	if i := strings.Index(t, " - "); i > 0 {
		t = t[:i]
	}
	t = bracketed.ReplaceAllString(t, " ")
	t = versionWords.ReplaceAllString(t, " ")
	t = nonWord.ReplaceAllString(t, " ")
	return strings.Join(strings.Fields(t), " ")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
	Session         []string // tracks played before the seed this session, newest first
	Skipped         []string // tracks skipped this session
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
	// Earlier songs in the session add their own related tracks, so the
	// picks follow the session's mood rather than a single song.
	sess := Session{Skipped: map[string]bool{}}
	for _, id := range req.Skipped {
		sess.Skipped[id] = true
	}
	if ids := firstN(req.Session, 5); len(ids) > 0 {
		if ts, err := s.cat.Tracks(ctx, ids); err == nil {
			byID := map[string]catalog.Track{}
			for _, t := range ts {
				byID[t.ID] = t
			}
			for _, id := range ids {
				if t, ok := byID[id]; ok && t.ID != seed.ID {
					sess.Recent = append(sess.Recent, t)
				}
			}
		} else {
			resp.Degraded = true
		}
		seeded := 0
		for _, t := range sess.Recent {
			if seeded == 2 {
				break
			}
			if t.Provider != catalog.ProviderSaavn || sess.Skipped[t.ID] {
				continue
			}
			id := t.ID
			seeded++
			pool(ReasonSessionRelated, func() ([]catalog.Track, error) { return s.cat.Related(ctx, id, 10) })
		}
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
	for _, id := range req.Session {
		exclude[id] = true
	}
	ranked := RankSession(seed, sess, cands, sig, exclude, s.now())
	ranked = Diversify(ranked, 1.5)
	ranked = ApplyArtistDiversity(ranked, 2)
	if len(ranked) > req.Limit {
		ranked = ranked[:req.Limit]
	}
	resp.Results = ranked
	return resp, nil
}

func firstN(ids []string, n int) []string {
	if len(ids) > n {
		return ids[:n]
	}
	return ids
}
