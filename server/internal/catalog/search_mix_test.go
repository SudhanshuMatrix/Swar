package catalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/testkit"
)

func ytSong(id, title, artist string) catalog.Track {
	return catalog.Track{ID: "yt:" + id, Provider: catalog.ProviderYouTube, ProviderID: id, Title: title,
		Artists: []catalog.ArtistRef{{Name: artist}}}
}

func TestMainSearchMixesInYouTubeMusic(t *testing.T) {
	cat, _ := testkit.Catalog(t, testkit.Options{})
	cat.SetExtraSongSearch(func(ctx context.Context, q string, limit int) ([]catalog.Track, error) {
		return []catalog.Track{
			ytSong("aaaaaaaaaaa", "Kesariya", "Arijit Singh"), // same song as Saavn's: dropped
			ytSong("bbbbbbbbbbb", "Kesariya Lofi", "Someone"),
		}, nil
	})
	p, err := cat.SearchSongs(context.Background(), "kesariya", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var yt, kesariya int
	for _, tr := range p.Results {
		if tr.Provider == catalog.ProviderYouTube {
			yt++
		}
		if tr.Title == "Kesariya" {
			kesariya++
		}
	}
	if yt != 1 || kesariya != 1 || p.Results[0].Provider == catalog.ProviderYouTube {
		t.Fatalf("want Saavn first and one YouTube Music extra, got %+v", p.Results)
	}
	// A YouTube Music song found in search can be looked up by ID later.
	if tr, err := cat.Track(context.Background(), "yt:bbbbbbbbbbb"); err != nil || tr.Title != "Kesariya Lofi" {
		t.Fatalf("remembered track: %v %v", tr, err)
	}
}

func TestMainSearchDoesNotWaitForSlowExtraSource(t *testing.T) {
	cat, _ := testkit.Catalog(t, testkit.Options{})
	cat.SetExtraSongSearch(func(ctx context.Context, q string, limit int) ([]catalog.Track, error) {
		<-ctx.Done()
		return nil, errors.New("too slow")
	})
	start := time.Now()
	p, err := cat.SearchSongs(context.Background(), "kesariya", 0, 10)
	if err != nil || len(p.Results) == 0 {
		t.Fatalf("search: %v %v", p, err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("search waited %v for the slow source", time.Since(start))
	}
}
