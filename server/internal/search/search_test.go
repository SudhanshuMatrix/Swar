package search

import (
	"context"
	"testing"

	"swar/internal/catalog"
	"swar/internal/testkit"
)

func TestDedupeByIdentityNotTitle(t *testing.T) {
	a := func(id, title, artist string, dur int) catalog.Track {
		return catalog.Track{ID: id, Title: title, DurationSec: dur, Artists: []catalog.ArtistRef{{Name: artist, Role: "primary"}}}
	}
	in := []catalog.Track{
		a("saavn:1", "Kesariya", "Arijit Singh", 268),
		a("saavn:1", "Kesariya", "Arijit Singh", 268), // same ID
		a("saavn:2", "Kesariya", "Arijit Singh", 269), // same recording, other listing
		a("saavn:3", "Kesariya", "Someone Else", 268), // same title, different artist
		a("saavn:4", "Kesariya (Remix)", "Arijit Singh", 200),
		a("saavn:5", "Kesariya", "Arijit Singh", 0), // unknown duration: cannot be sure
	}
	out := DedupeTracks(in)
	ids := []string{}
	for _, t := range out {
		ids = append(ids, t.ID)
	}
	want := []string{"saavn:1", "saavn:3", "saavn:4", "saavn:5"}
	if len(ids) != len(want) {
		t.Fatalf("got %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v want %v", ids, want)
		}
	}
}

func TestUnifiedSearch(t *testing.T) {
	cat, _ := testkit.Catalog(t, testkit.Options{})
	s := New(cat)
	r, err := s.Search(context.Background(), "  kesariya   song ", TypeAll, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Query != "kesariya song" {
		t.Errorf("query not normalized: %q", r.Query)
	}
	if r.Songs == nil || len(r.Songs.Results) != 2 {
		t.Fatalf("songs should be deduped to 2: %+v", r.Songs)
	}
	if r.Albums == nil || r.Artists == nil || r.Playlists == nil || r.Partial {
		t.Fatalf("all categories expected: %+v", r)
	}
}

func TestPartialFailure(t *testing.T) {
	cat, f := testkit.Catalog(t, testkit.Options{})
	f.FailNext("search.getAlbumResults", 500)
	r, err := New(cat).Search(context.Background(), "x", TypeAll, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Partial || r.Albums != nil || r.Errors["albums"] == "" || r.Songs == nil {
		t.Fatalf("want partial result: %+v", r)
	}
}

func TestTotalFailureKeepsErrorKind(t *testing.T) {
	cat, f := testkit.Catalog(t, testkit.Options{})
	f.FailNext("search.getResults", 429)
	_, err := New(cat).Search(context.Background(), "x", TypeSongs, 0, 5)
	if catalog.KindOf(err) != catalog.KindRateLimited {
		t.Fatalf("want rate_limited, got %v", err)
	}
}

func TestValidation(t *testing.T) {
	cat, _ := testkit.Catalog(t, testkit.Options{})
	s := New(cat)
	for _, q := range []string{"", "   ", "bad\x00query", string(make([]byte, 300))} {
		if _, err := s.Search(context.Background(), q, TypeAll, 0, 5); catalog.KindOf(err) != catalog.KindInvalid {
			t.Errorf("query %q: want invalid, got %v", q, err)
		}
	}
	if _, ok := ParseType("videos"); ok {
		t.Error("unknown type accepted")
	}
}
