package music

import (
	"context"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/providers"
	"swar/internal/music/search"
)

type dummyProvider struct {
	name   string
	tracks []catalog.Track
}

func (d *dummyProvider) Name() string { return d.name }
func (d *dummyProvider) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error) {
	return d.tracks, nil
}
func (d *dummyProvider) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error) {
	return []catalog.Album{{ID: d.name + ":alb1", Name: "Album 1"}}, nil
}
func (d *dummyProvider) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error) {
	return []catalog.ArtistRef{{ID: d.name + ":art1", Name: "Artist 1"}}, nil
}
func (d *dummyProvider) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error) {
	return []catalog.Playlist{{ID: d.name + ":pl1", Name: "Playlist 1"}}, nil
}
func (d *dummyProvider) Track(ctx context.Context, id string) (*catalog.Track, error) {
	return &catalog.Track{ID: d.name + ":" + id, Provider: d.name, ProviderID: id, Title: "Track " + id}, nil
}
func (d *dummyProvider) Album(ctx context.Context, id string) (*catalog.Album, error) {
	return &catalog.Album{ID: d.name + ":" + id, Name: "Album " + id}, nil
}
func (d *dummyProvider) Artist(ctx context.Context, id string) (*catalog.Artist, error) {
	return &catalog.Artist{ID: d.name + ":" + id, Name: "Artist " + id}, nil
}
func (d *dummyProvider) ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	return d.tracks, nil
}
func (d *dummyProvider) ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error) {
	return []catalog.Album{}, nil
}
func (d *dummyProvider) Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error) {
	return &catalog.Playlist{ID: d.name + ":" + id, Name: "Playlist " + id}, nil
}
func (d *dummyProvider) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}
func (d *dummyProvider) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}
func (d *dummyProvider) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	return []catalog.MediaSource{
		{Kind: "audio", Quality: "128kbps", URL: "https://preview.com/" + id + ".mp3"},
	}, time.Now().Add(time.Hour), nil
}

func TestEngineFederatedSearchAndTracks(t *testing.T) {
	p1 := &dummyProvider{
		name: "dz",
		tracks: []catalog.Track{
			{ID: "dz:1", Provider: "dz", Title: "Kesariya", Artists: []catalog.ArtistRef{{Name: "Arijit Singh"}}},
		},
	}
	p2 := &dummyProvider{
		name: "it",
		tracks: []catalog.Track{
			{ID: "it:2", Provider: "it", Title: "Apna Bana Le", Artists: []catalog.ArtistRef{{Name: "Arijit Singh"}}},
		},
	}

	eng := NewEngine(EngineOptions{
		SearchTimeout: 2 * time.Second,
	})
	eng.providers["dz"] = p1
	eng.providers["it"] = p2
	eng.prvList = []providers.Provider{p1, p2}
	eng.federated = search.NewFederatedSearch([]providers.Provider{p1, p2}, 2*time.Second)

	// Search Songs
	p, err := eng.SearchSongs(context.Background(), "Arijit", 0, 10)
	if err != nil {
		t.Fatalf("SearchSongs failed: %v", err)
	}
	if len(p.Results) != 2 {
		t.Fatalf("expected 2 songs from federated search, got %d", len(p.Results))
	}

	// Tracks lookup with multi-provider routing
	ts, err := eng.Tracks(context.Background(), []string{"dz:100", "it:200", "saavn:legacy123"})
	if err != nil {
		t.Fatalf("Tracks failed: %v", err)
	}
	if len(ts) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(ts))
	}

	// Verify legacy saavn track handled safely
	var legacy *catalog.Track
	for i := range ts {
		if ts[i].ID == "saavn:legacy123" {
			legacy = &ts[i]
		}
	}
	if legacy == nil {
		t.Fatalf("expected legacy saavn track to be returned")
	}
	if legacy.Provider != "saavn" || legacy.ProviderID != "legacy123" {
		t.Errorf("unexpected legacy track: %+v", legacy)
	}

	// MediaSources routing
	sources, _, err := eng.MediaSources(context.Background(), "dz:100")
	if err != nil {
		t.Fatalf("MediaSources failed: %v", err)
	}
	if len(sources) != 1 || sources[0].URL != "https://preview.com/100.mp3" {
		t.Errorf("unexpected sources: %+v", sources)
	}
}
