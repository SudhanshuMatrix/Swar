package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/providers"
)

type mockProvider struct {
	name      string
	tracks    []catalog.Track
	albums    []catalog.Album
	artists   []catalog.ArtistRef
	playlists []catalog.Playlist
	err       error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error) {
	return m.tracks, m.err
}
func (m *mockProvider) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error) {
	return m.albums, m.err
}
func (m *mockProvider) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error) {
	return m.artists, m.err
}
func (m *mockProvider) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error) {
	return m.playlists, m.err
}
func (m *mockProvider) Track(ctx context.Context, id string) (*catalog.Track, error) {
	return nil, nil
}
func (m *mockProvider) Album(ctx context.Context, id string) (*catalog.Album, error) {
	return nil, nil
}
func (m *mockProvider) Artist(ctx context.Context, id string) (*catalog.Artist, error) {
	return nil, nil
}
func (m *mockProvider) ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	return nil, nil
}
func (m *mockProvider) ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error) {
	return nil, nil
}
func (m *mockProvider) Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error) {
	return nil, nil
}
func (m *mockProvider) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	return nil, nil
}
func (m *mockProvider) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	return nil, nil
}

func TestDeduplicationAndCanonicalMerge(t *testing.T) {
	dzTrack := catalog.Track{
		ID:          "dz:101",
		Provider:    "dz",
		ProviderID:  "101",
		Title:       "Kesariya",
		Artists:     []catalog.ArtistRef{{Name: "Pritam"}},
		DurationSec: 268,
		Playable:    true,
		ProviderMeta: map[string]string{
			"isrc":        "INS172200547",
			"preview_url": "https://dz.cdn/preview.mp3",
		},
	}

	itTrack := catalog.Track{
		ID:          "it:202",
		Provider:    "it",
		ProviderID:  "202",
		Title:       "Kesariya (From Brahmastra)",
		Artists:     []catalog.ArtistRef{{Name: "Pritam"}, {Name: "Arijit Singh"}},
		DurationSec: 268,
		Images: []catalog.Image{
			{Quality: "1000x1000", URL: "https://apple.com/1000x1000.jpg"},
		},
		ProviderMeta: map[string]string{
			"isrc":      "INS172200547",
			"itunes_id": "202",
		},
	}

	merged := DeduplicateAndMergeTracks([]catalog.Track{dzTrack, itTrack})
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged canonical track, got %d", len(merged))
	}

	res := merged[0]
	if res.Title != "Kesariya" {
		t.Errorf("expected clean title Kesariya, got %s", res.Title)
	}
	if len(res.Artists) != 2 {
		t.Errorf("expected 2 artists from richer metadata, got %d", len(res.Artists))
	}
	if len(res.Images) != 1 || res.Images[0].Quality != "1000x1000" {
		t.Errorf("expected Apple 1000x1000 image, got %+v", res.Images)
	}
	if !res.Playable {
		t.Errorf("expected playable true")
	}
	if res.ProviderMeta["isrc"] != "INS172200547" {
		t.Errorf("expected ISRC preserved")
	}
	if res.ProviderMeta["sources"] != "dz,it" {
		t.Errorf("expected provenance dz,it, got %s", res.ProviderMeta["sources"])
	}
}

func TestDeduplicationPreservesDistinctVersions(t *testing.T) {
	original := catalog.Track{
		ID:          "dz:1",
		Provider:    "dz",
		Title:       "Kesariya",
		Artists:     []catalog.ArtistRef{{Name: "Arijit Singh"}},
		DurationSec: 268,
	}
	remix := catalog.Track{
		ID:          "dz:2",
		Provider:    "dz",
		Title:       "Kesariya (Remix)",
		Artists:     []catalog.ArtistRef{{Name: "Arijit Singh"}},
		DurationSec: 240,
	}
	acoustic := catalog.Track{
		ID:          "dz:3",
		Provider:    "dz",
		Title:       "Kesariya - Acoustic Version",
		Artists:     []catalog.ArtistRef{{Name: "Arijit Singh"}},
		DurationSec: 275,
	}
	live := catalog.Track{
		ID:          "dz:4",
		Provider:    "dz",
		Title:       "Kesariya [Live in Concert]",
		Artists:     []catalog.ArtistRef{{Name: "Arijit Singh"}},
		DurationSec: 310,
	}

	tracks := []catalog.Track{original, remix, acoustic, live}
	deduped := DeduplicateAndMergeTracks(tracks)

	if len(deduped) != 4 {
		t.Fatalf("expected 4 distinct versions preserved, got %d", len(deduped))
	}
}

func TestRanking(t *testing.T) {
	t1 := catalog.Track{
		ID:       "dz:1",
		Provider: "dz",
		Title:    "Kesariya",
		Artists:  []catalog.ArtistRef{{Name: "Arijit Singh"}},
	}
	t2 := catalog.Track{
		ID:       "dz:2",
		Provider: "dz",
		Title:    "Kesariya (Remix)",
		Artists:  []catalog.ArtistRef{{Name: "DJ Chetas"}},
	}
	t3 := catalog.Track{
		ID:       "dz:3",
		Provider: "dz",
		Title:    "Something Else Mentioning Kesariya In Notes",
		Artists:  []catalog.ArtistRef{{Name: "Other"}},
	}

	// 1. Regular search: original track must rank #1
	ranked := RankTracks([]catalog.Track{t3, t2, t1}, "Kesariya")
	if ranked[0].ID != "dz:1" {
		t.Errorf("expected original Kesariya first, got %s", ranked[0].Title)
	}

	// 2. Query explicitly asking for remix: remix track must rank #1
	rankedRemix := RankTracks([]catalog.Track{t1, t2}, "Kesariya Remix")
	if rankedRemix[0].ID != "dz:2" {
		t.Errorf("expected remix first when searched for remix, got %s", rankedRemix[0].Title)
	}
}

func TestFederatedSearchWithPartialFailure(t *testing.T) {
	p1 := &mockProvider{
		name: "dz",
		tracks: []catalog.Track{
			{ID: "dz:1", Provider: "dz", Title: "Tum Hi Ho", Artists: []catalog.ArtistRef{{Name: "Arijit Singh"}}},
		},
	}
	p2 := &mockProvider{
		name: "it",
		err:  errors.New("connection timeout"),
	}

	fed := NewFederatedSearch([]providers.Provider{p1, p2}, 5*time.Second)
	tracks, errs := fed.SearchTracks(context.Background(), "Tum Hi Ho", 10)

	if len(tracks) != 1 {
		t.Fatalf("expected 1 track from successful provider, got %d", len(tracks))
	}
	if tracks[0].Title != "Tum Hi Ho" {
		t.Errorf("unexpected track: %s", tracks[0].Title)
	}
	if errs["it"] == nil {
		t.Errorf("expected error from provider 'it' recorded in errors map")
	}
}

func TestMainSearchUsesSpotifyAndDeepSearchAll(t *testing.T) {
	sp := &mockProvider{name: catalog.ProviderSpotify, tracks: []catalog.Track{{ID: "sp:1", Provider: catalog.ProviderSpotify, Title: "Kesariya"}}}
	dz := &mockProvider{name: catalog.ProviderDeezer, tracks: []catalog.Track{{ID: "dz:1", Provider: catalog.ProviderDeezer, Title: "Kesariya Rangu"}}}
	mb := &mockProvider{name: catalog.ProviderMusicBrainz, tracks: []catalog.Track{{ID: "mb:1", Provider: catalog.ProviderMusicBrainz, Title: "Kesariya Live"}}}
	fed := NewFederatedSearch([]providers.Provider{sp, dz, mb}, 5*time.Second)

	main, _ := fed.SearchTracksFast(context.Background(), "Kesariya", 10)
	if len(main) != 1 || main[0].Provider != catalog.ProviderSpotify {
		t.Fatalf("main search should come from Spotify only, got %+v", main)
	}
	deep, _ := fed.SearchTracks(context.Background(), "Kesariya", 10)
	if len(deep) < 2 || deep[0].ProviderMeta["sources"] != "sp,dz" {
		t.Fatalf("deep search should ask every provider, got %+v", deep)
	}

	// Spotify failing falls back to the fast zero-key providers.
	sp.tracks, sp.err = nil, errors.New("401")
	main, _ = fed.SearchTracksFast(context.Background(), "Kesariya", 10)
	if len(main) != 1 || main[0].Provider != catalog.ProviderDeezer {
		t.Fatalf("fallback should use Deezer, got %+v", main)
	}
}
