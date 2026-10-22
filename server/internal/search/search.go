// Package search implements unified catalog search across songs, albums,
// artists and playlists, with stable-identity deduplication.
package search

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"swar/internal/catalog"
)

type Type string

const (
	TypeAll       Type = "all"
	TypeSongs     Type = "songs"
	TypeAlbums    Type = "albums"
	TypeArtists   Type = "artists"
	TypePlaylists Type = "playlists"
)

func ParseType(s string) (Type, bool) {
	switch Type(s) {
	case "", TypeAll:
		return TypeAll, true
	case TypeSongs, TypeAlbums, TypeArtists, TypePlaylists:
		return Type(s), true
	}
	return "", false
}

// Result holds per-category pages. A category that failed is nil and listed
// in Errors; the others are still returned (Partial is then true).
type Result struct {
	Query     string                           `json:"query"`
	Songs     *catalog.Page[catalog.Track]     `json:"songs,omitempty"`
	Albums    *catalog.Page[catalog.Album]     `json:"albums,omitempty"`
	Artists   *catalog.Page[catalog.ArtistRef] `json:"artists,omitempty"`
	Playlists *catalog.Page[catalog.Playlist]  `json:"playlists,omitempty"`
	Partial   bool                             `json:"partial"`
	Errors    map[string]string                `json:"errors,omitempty"`
}

type Service struct {
	cat *catalog.Service
}

func New(cat *catalog.Service) *Service { return &Service{cat: cat} }

// Search runs the requested categories concurrently. Limit is clamped to
// [1, 50]; for "all" each category gets the same limit.
func (s *Service) Search(ctx context.Context, rawQuery string, typ Type, page, limit int) (*Result, error) {
	q, err := catalog.NormalizeQuery(rawQuery)
	if err != nil {
		return nil, err
	}
	limit = clamp(limit, 1, 100)
	page = clamp(page, 0, 100)
	r := &Result{Query: q}

	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := map[string]error{}
	fail := func(cat string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if r.Errors == nil {
			r.Errors = map[string]string{}
		}
		r.Errors[cat] = safeMessage(err)
		errs[cat] = err
	}
	run := func(t Type, fn func() error) {
		if typ != TypeAll && typ != t {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				fail(string(t), err)
			}
		}()
	}
	run(TypeSongs, func() error {
		p, err := s.cat.SearchSongs(ctx, q, page, limit)
		if err == nil {
			cp := *p
			cp.Results = DedupeTracks(p.Results)
			mu.Lock()
			r.Songs = &cp
			mu.Unlock()
		}
		return err
	})
	run(TypeAlbums, func() error {
		p, err := s.cat.SearchAlbums(ctx, q, page, limit)
		if err == nil {
			cp := *p
			cp.Results = dedupeBy(p.Results, func(a catalog.Album) string { return a.ID })
			mu.Lock()
			r.Albums = &cp
			mu.Unlock()
		}
		return err
	})
	run(TypeArtists, func() error {
		p, err := s.cat.SearchArtists(ctx, q, page, limit)
		if err == nil {
			cp := *p
			cp.Results = dedupeBy(p.Results, func(a catalog.ArtistRef) string { return a.ID })
			if typ == TypeArtists {
				cp.Results = s.rankArtistsBySongs(ctx, cp.Results)
			}
			mu.Lock()
			r.Artists = &cp
			mu.Unlock()
		}
		return err
	})
	run(TypePlaylists, func() error {
		p, err := s.cat.SearchPlaylists(ctx, q, page, limit)
		if err == nil {
			cp := *p
			cp.Results = dedupeBy(p.Results, func(a catalog.Playlist) string { return a.ID })
			mu.Lock()
			r.Playlists = &cp
			mu.Unlock()
		}
		return err
	})
	wg.Wait()

	if len(r.Errors) > 0 {
		if r.Songs == nil && r.Albums == nil && r.Artists == nil && r.Playlists == nil {
			// Nothing succeeded: return a real error, preferring songs.
			for _, k := range []Type{TypeSongs, TypeAlbums, TypeArtists, TypePlaylists} {
				if err := errs[string(k)]; err != nil {
					return nil, err
				}
			}
		}
		r.Partial = true
	}
	return r, nil
}

// rankArtistsBySongs looks up how many songs each artist has, drops the
// ones with none, and puts the biggest catalogs first. Artists whose count
// could not be fetched are kept after the counted ones, so a slow upstream
// never empties the list. Only the artists-only search pays for this.
func (s *Service) rankArtistsBySongs(ctx context.Context, in []catalog.ArtistRef) []catalog.ArtistRef {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out := make([]catalog.ArtistRef, len(in))
	copy(out, in)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range out {
		wg.Add(1)
		go func(a *catalog.ArtistRef) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p, err := s.cat.ArtistSongs(ctx, a.ID, catalog.ArtistOptions{})
			if err != nil {
				return
			}
			n := p.Total
			if n < len(p.Results) {
				n = len(p.Results)
			}
			a.SongCount = &n
		}(&out[i])
	}
	wg.Wait()
	kept := out[:0]
	for _, a := range out {
		if a.SongCount == nil || *a.SongCount > 0 {
			kept = append(kept, a)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		ci, cj := -1, -1
		if kept[i].SongCount != nil {
			ci = *kept[i].SongCount
		}
		if kept[j].SongCount != nil {
			cj = *kept[j].SongCount
		}
		return ci > cj
	})
	return kept
}

func safeMessage(err error) string {
	var ce *catalog.Error
	if errors.As(err, &ce) {
		return ce.Message
	}
	return "search failed"
}

// DedupeTracks removes duplicates by stable identity: first the internal
// ID, then normalized title + primary artists with durations within two
// seconds, which catches the same recording listed twice by the provider.
// Title alone is never used, so different songs with the same name survive,
// and tracks without enough metadata are always kept.
func DedupeTracks(in []catalog.Track) []catalog.Track {
	seenID := make(map[string]bool, len(in))
	seenDur := make(map[string][]int, len(in))
	out := make([]catalog.Track, 0, len(in))
	for _, t := range in {
		if t.ID == "" || seenID[t.ID] {
			continue
		}
		fp := Fingerprint(t)
		if fp != "" {
			dup := false
			for _, d := range seenDur[fp] {
				if abs(d-t.DurationSec) <= 2 {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			seenDur[fp] = append(seenDur[fp], t.DurationSec)
		}
		seenID[t.ID] = true
		out = append(out, t)
	}
	return out
}

// Fingerprint identifies a recording by normalized title and artists. It
// returns "" when there is not enough metadata to be sure (no artists or
// no duration), in which case the track is never treated as a duplicate.
func Fingerprint(t catalog.Track) string {
	title := normalize(t.Title)
	artists := normalize(t.ArtistNames())
	if title == "" || artists == "" || t.DurationSec <= 0 {
		return ""
	}
	return title + "|" + artists
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dedupeBy[T any](in []T, key func(T) string) []T {
	seen := make(map[string]bool, len(in))
	out := make([]T, 0, len(in))
	for _, v := range in {
		k := key(v)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
