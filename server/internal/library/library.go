// Package library implements per-device liked songs, dislikes, playlists,
// listening history and preferences on top of the repository.
//
// The Flutter app stays local-first (Hive); these endpoints add optional
// server-side copies that power recommendations and radio. History is
// private to the device, bounded in size, and can be wiped by the client.
package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"swar/internal/catalog"
	"swar/internal/recommendations"
	"swar/internal/repository"
)

type Service struct {
	store *repository.Store
	cat   *catalog.Service
	now   func() time.Time
}

func New(store *repository.Store, cat *catalog.Service) *Service {
	return &Service{store: store, cat: cat, now: time.Now}
}

func validTrackID(id string) (string, error) {
	p, pid, ok := catalog.ParseID(id)
	if !ok {
		return "", catalog.Invalid("invalid track id")
	}
	return catalog.MakeID(p, pid), nil
}

// snapshot stores track metadata so the library renders without upstream
// calls. Best effort: the like still succeeds if metadata is unavailable.
func (s *Service) snapshot(ctx context.Context, ids ...string) {
	known, _ := s.store.Tracks(ctx, ids)
	var missing []string
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return
	}
	if ts, err := s.cat.Tracks(ctx, missing); err == nil {
		_ = s.store.SaveTracks(ctx, ts...)
	}
}

// Hydrate returns tracks for ids in order. Unknown tracks are returned with
// just their ID so the client can still show and remove them.
func (s *Service) Hydrate(ctx context.Context, ids []string) ([]catalog.Track, error) {
	known, err := s.store.Tracks(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := known[id]; ok {
			out = append(out, t)
			continue
		}
		p, pid, _ := catalog.ParseID(id)
		out = append(out, catalog.Track{ID: id, Provider: p, ProviderID: pid, Artists: []catalog.ArtistRef{}, Images: []catalog.Image{}})
	}
	return out, nil
}

func (s *Service) React(ctx context.Context, device, trackID string, r repository.Reaction, on bool) error {
	id, err := validTrackID(trackID)
	if err != nil {
		return err
	}
	if !on {
		err := s.store.ClearReaction(ctx, device, id, r)
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.store.SetReaction(ctx, device, id, r); err != nil {
		return err
	}
	s.snapshot(ctx, id)
	return nil
}

func (s *Service) Reactions(ctx context.Context, device string, r repository.Reaction, limit int) ([]catalog.Track, error) {
	ids, err := s.store.Reactions(ctx, device, r, clamp(limit, 1, 1000))
	if err != nil {
		return nil, err
	}
	return s.Hydrate(ctx, ids)
}

// ---- playlists ----

type Playlist struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	TrackIDs  []string        `json:"trackIds"`
	Tracks    []catalog.Track `json:"tracks,omitempty"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

func validName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len([]rune(name)) > 100 {
		return "", catalog.Invalid("playlist name must be 1-100 characters")
	}
	return name, nil
}

func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) CreatePlaylist(ctx context.Context, device, name string, trackIDs []string) (*Playlist, error) {
	name, err := validName(name)
	if err != nil {
		return nil, err
	}
	ids, err := s.validIDs(trackIDs)
	if err != nil {
		return nil, err
	}
	p, err := s.store.CreatePlaylist(ctx, device, newID("pl_"), name)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		if err := s.store.SetPlaylistTracks(ctx, device, p.ID, ids); err != nil {
			return nil, err
		}
		s.snapshot(ctx, ids...)
	}
	return s.Playlist(ctx, device, p.ID, false)
}

func (s *Service) Playlists(ctx context.Context, device string) ([]Playlist, error) {
	ps, err := s.store.Playlists(ctx, device)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, 0, len(ps))
	for _, p := range ps {
		out = append(out, Playlist{ID: p.ID, Name: p.Name, TrackIDs: p.TrackIDs, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt})
	}
	return out, nil
}

func (s *Service) Playlist(ctx context.Context, device, id string, withTracks bool) (*Playlist, error) {
	p, err := s.store.Playlist(ctx, device, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, catalog.NotFound("playlist not found")
	}
	if err != nil {
		return nil, err
	}
	out := &Playlist{ID: p.ID, Name: p.Name, TrackIDs: p.TrackIDs, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	if withTracks {
		if out.Tracks, err = s.Hydrate(ctx, p.TrackIDs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) RenamePlaylist(ctx context.Context, device, id, name string) error {
	name, err := validName(name)
	if err != nil {
		return err
	}
	return notFound(s.store.RenamePlaylist(ctx, device, id, name), "playlist not found")
}

func (s *Service) DeletePlaylist(ctx context.Context, device, id string) error {
	return notFound(s.store.DeletePlaylist(ctx, device, id), "playlist not found")
}

// SetPlaylistTracks replaces the playlist's ordered track list.
func (s *Service) SetPlaylistTracks(ctx context.Context, device, id string, trackIDs []string) error {
	ids, err := s.validIDs(trackIDs)
	if err != nil {
		return err
	}
	if err := notFound(s.store.SetPlaylistTracks(ctx, device, id, ids), "playlist not found"); err != nil {
		return err
	}
	s.snapshot(ctx, ids...)
	return nil
}

func (s *Service) validIDs(in []string) ([]string, error) {
	if len(in) > repository.MaxPlaylistTracks {
		return nil, catalog.Invalid("too many tracks")
	}
	out := make([]string, 0, len(in))
	for _, id := range in {
		v, err := validTrackID(id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ---- history ----

const maxHistoryBatch = 100

func (s *Service) RecordHistory(ctx context.Context, device string, events []repository.HistoryEvent) error {
	if len(events) == 0 {
		return nil
	}
	if len(events) > maxHistoryBatch {
		return catalog.Invalid("at most 100 history events per request")
	}
	now := s.now().UnixMilli()
	ids := make([]string, 0, len(events))
	for i := range events {
		e := &events[i]
		id, err := validTrackID(e.TrackID)
		if err != nil {
			return err
		}
		e.TrackID = id
		if e.PlayedAt == 0 {
			e.PlayedAt = now
		}
		if e.PlayedAt > now+5*60_000 || e.PlayedAt < now-30*24*3600_000 {
			return catalog.Invalid("playedAt must be within the last 30 days")
		}
		if e.MsPlayed < 0 || e.MsPlayed > 24*3600_000 {
			return catalog.Invalid("msPlayed is out of range")
		}
		if len(e.Context) > 64 {
			e.Context = e.Context[:64]
		}
		ids = append(ids, id)
	}
	if err := s.store.AddHistory(ctx, device, events); err != nil {
		return err
	}
	s.snapshot(ctx, ids...)
	return nil
}

type HistoryItem struct {
	repository.HistoryEvent
	Track catalog.Track `json:"track"`
}

func (s *Service) History(ctx context.Context, device string, limit int) ([]HistoryItem, error) {
	evs, err := s.store.History(ctx, device, clamp(limit, 1, 500))
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(evs))
	for i, e := range evs {
		ids[i] = e.TrackID
	}
	ts, err := s.Hydrate(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryItem, len(evs))
	for i := range evs {
		out[i] = HistoryItem{HistoryEvent: evs[i], Track: ts[i]}
	}
	return out, nil
}

func (s *Service) Recent(ctx context.Context, device string, limit int) ([]catalog.Track, error) {
	ids, err := s.store.RecentTracks(ctx, device, clamp(limit, 1, 100))
	if err != nil {
		return nil, err
	}
	return s.Hydrate(ctx, ids)
}

func (s *Service) ClearHistory(ctx context.Context, device string) error {
	return s.store.ClearHistory(ctx, device)
}

// ---- preferences ----

// Allowed preference keys and validators. Unknown keys are rejected so the
// table cannot become a dumping ground for arbitrary client data.
var prefValidators = map[string]func(string) bool{
	"languages":     func(v string) bool { return len(v) <= 200 },
	"explicit":      func(v string) bool { return v == "allow" || v == "block" },
	"dataSaver":     isBool,
	"preferOffline": isBool,
	"streamQuality": func(v string) bool { return v == "low" || v == "medium" || v == "high" },
}

func isBool(v string) bool { _, err := strconv.ParseBool(v); return err == nil }

func (s *Service) Preferences(ctx context.Context, device string) (map[string]string, error) {
	return s.store.Preferences(ctx, device)
}

func (s *Service) SetPreferences(ctx context.Context, device string, prefs map[string]string) error {
	if len(prefs) > len(prefValidators) {
		return catalog.Invalid("too many preferences")
	}
	for k, v := range prefs {
		valid, ok := prefValidators[k]
		if !ok {
			return catalog.Invalid("unknown preference: " + k)
		}
		if v != "" && !valid(v) {
			return catalog.Invalid("invalid value for " + k)
		}
	}
	return s.store.SetPreferences(ctx, device, prefs)
}

// Signals implements recommendations.SignalSource.
func (s *Service) Signals(ctx context.Context, device string) (recommendations.Signals, error) {
	sig := recommendations.Signals{
		Liked: map[string]bool{}, Disliked: map[string]bool{}, LikedArtists: map[string]int{},
		RecentPlays: map[string]time.Time{}, Skips: map[string]int{}, Languages: map[string]bool{},
	}
	liked, err := s.store.Reactions(ctx, device, repository.Like, 500)
	if err != nil {
		return sig, err
	}
	for _, id := range liked {
		sig.Liked[id] = true
	}
	if meta, err := s.store.Tracks(ctx, liked); err == nil {
		for _, t := range meta {
			for _, a := range t.ArtistIDs() {
				sig.LikedArtists[a]++
			}
		}
	}
	disliked, err := s.store.Reactions(ctx, device, repository.Dislike, 500)
	if err != nil {
		return sig, err
	}
	for _, id := range disliked {
		sig.Disliked[id] = true
	}
	hist, err := s.store.History(ctx, device, 200)
	if err != nil {
		return sig, err
	}
	for _, h := range hist {
		at := time.UnixMilli(h.PlayedAt)
		if prev, ok := sig.RecentPlays[h.TrackID]; !ok || at.After(prev) {
			sig.RecentPlays[h.TrackID] = at
		}
	}
	if skips, err := s.store.SkipCounts(ctx, device, s.now().Add(-30*24*time.Hour).UnixMilli()); err == nil {
		sig.Skips = skips
	}
	if prefs, err := s.store.Preferences(ctx, device); err == nil {
		for _, l := range strings.Split(prefs["languages"], ",") {
			if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
				sig.Languages[l] = true
			}
		}
		sig.BlockExplicit = prefs["explicit"] == "block"
	}
	return sig, nil
}

func notFound(err error, msg string) error {
	if errors.Is(err, repository.ErrNotFound) {
		return catalog.NotFound(msg)
	}
	return err
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
