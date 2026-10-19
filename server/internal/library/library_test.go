package library

import (
	"context"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/repository"
	"swar/internal/testkit"
)

func setup(t *testing.T) (*Service, string) {
	cat, _ := testkit.Catalog(t, testkit.Options{})
	st := testkit.Store(t)
	return New(st, cat), testkit.Device(t, st, "dev_1")
}

func TestLikesAndDislikesAreExclusive(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()
	if err := s.React(ctx, d, "saavn:s1", repository.Like, true); err != nil {
		t.Fatal(err)
	}
	if err := s.React(ctx, d, "s2", repository.Like, true); err != nil { // bare id normalized
		t.Fatal(err)
	}
	if err := s.React(ctx, d, "saavn:s1", repository.Dislike, true); err != nil {
		t.Fatal(err)
	}
	likes, _ := s.Reactions(ctx, d, repository.Like, 10)
	dislikes, _ := s.Reactions(ctx, d, repository.Dislike, 10)
	if len(likes) != 1 || likes[0].ID != "saavn:s2" || likes[0].Title == "" {
		t.Fatalf("likes (with metadata snapshot): %+v", likes)
	}
	if len(dislikes) != 1 || dislikes[0].ID != "saavn:s1" {
		t.Fatalf("dislikes: %+v", dislikes)
	}
	if err := s.React(ctx, d, "../etc", repository.Like, true); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("invalid id accepted")
	}
}

func TestPlaylistsOrderAndIsolation(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()
	p, err := s.CreatePlaylist(ctx, d, "  Road   trip ", []string{"saavn:s1", "saavn:s2"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Road trip" {
		t.Errorf("name not normalized: %q", p.Name)
	}
	if err := s.SetPlaylistTracks(ctx, d, p.ID, []string{"saavn:s2", "saavn:s1", "saavn:s2"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Playlist(ctx, d, p.ID, true)
	if len(got.TrackIDs) != 3 || got.TrackIDs[0] != "saavn:s2" || got.Tracks[1].ID != "saavn:s1" {
		t.Fatalf("order: %+v", got.TrackIDs)
	}
	// Another device cannot see or change it.
	other := "dev_2"
	_ = s.store.CreateDevice(ctx, other)
	if _, err := s.Playlist(ctx, other, p.ID, false); catalog.KindOf(err) != catalog.KindNotFound {
		t.Fatal("playlist leaked across devices")
	}
	if err := s.RenamePlaylist(ctx, other, p.ID, "x"); catalog.KindOf(err) != catalog.KindNotFound {
		t.Fatal("rename across devices")
	}
	if _, err := s.CreatePlaylist(ctx, d, "", nil); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("empty name accepted")
	}
	if err := s.DeletePlaylist(ctx, d, p.ID); err != nil {
		t.Fatal(err)
	}
	if ps, _ := s.Playlists(ctx, d); len(ps) != 0 {
		t.Fatal("not deleted")
	}
}

func TestHistoryValidationRetentionAndSignals(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	err := s.RecordHistory(ctx, d, []repository.HistoryEvent{
		{TrackID: "saavn:s1", PlayedAt: now - 1000, MsPlayed: 60000},
		{TrackID: "saavn:s2", PlayedAt: now, MsPlayed: 3000, Skipped: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHistory(ctx, d, []repository.HistoryEvent{{TrackID: "saavn:s1", PlayedAt: now - 90*24*3600_000}}); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("ancient event accepted")
	}
	if err := s.RecordHistory(ctx, d, []repository.HistoryEvent{{TrackID: "saavn:s1", MsPlayed: -1}}); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("negative msPlayed accepted")
	}
	recent, _ := s.Recent(ctx, d, 10)
	if len(recent) != 2 || recent[0].ID != "saavn:s2" {
		t.Fatalf("recent: %+v", recent)
	}
	_ = s.React(ctx, d, "saavn:s1", repository.Like, true)
	_ = s.SetPreferences(ctx, d, map[string]string{"languages": "Hindi, Punjabi", "explicit": "block"})
	sig, err := s.Signals(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if !sig.Liked["saavn:s1"] || sig.Skips["saavn:s2"] != 1 || !sig.Languages["punjabi"] || !sig.BlockExplicit || sig.LikedArtists["saavn:a2"] != 1 {
		t.Fatalf("signals: %+v", sig)
	}
	_ = s.ClearHistory(ctx, d)
	if h, _ := s.History(ctx, d, 10); len(h) != 0 {
		t.Fatal("history not cleared")
	}
}

func TestPreferencesValidation(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()
	for _, p := range []map[string]string{{"unknown": "x"}, {"explicit": "maybe"}, {"dataSaver": "perhaps"}} {
		if err := s.SetPreferences(ctx, d, p); catalog.KindOf(err) != catalog.KindInvalid {
			t.Errorf("%v accepted", p)
		}
	}
	if err := s.SetPreferences(ctx, d, map[string]string{"dataSaver": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPreferences(ctx, d, map[string]string{"dataSaver": ""}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Preferences(ctx, d); len(p) != 0 {
		t.Fatalf("empty value should delete: %v", p)
	}
}

func TestDeleteDeviceCascades(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()
	_ = s.React(ctx, d, "saavn:s1", repository.Like, true)
	_, _ = s.CreatePlaylist(ctx, d, "x", []string{"saavn:s1"})
	if err := s.store.DeleteDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.store.Reactions(ctx, d, repository.Like, 10); len(ids) != 0 {
		t.Fatal("likes survived device deletion")
	}
}
