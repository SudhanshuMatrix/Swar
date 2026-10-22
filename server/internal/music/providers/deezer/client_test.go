package deezer

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/providers"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mockHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{
		Transport: fn,
	}
}

func TestDeezerSearchTracks(t *testing.T) {
	mockJSON := `{
		"data": [
			{
				"id": 1827471677,
				"title": "Kesariya",
				"title_short": "Kesariya",
				"link": "https://www.deezer.com/track/1827471677",
				"duration": 268,
				"rank": 950000,
				"explicit_lyrics": false,
				"preview": "https://cdns-preview-d.dzcdn.net/stream/sample.mp3",
				"artist": {
					"id": 4443585,
					"name": "Pritam",
					"link": "https://www.deezer.com/artist/4443585",
					"picture_small": "https://e-cdns-images.dzcdn.net/images/artist/small.jpg",
					"picture_xl": "https://e-cdns-images.dzcdn.net/images/artist/xl.jpg"
				},
				"album": {
					"id": 339462717,
					"title": "Brahmastra",
					"cover_small": "https://e-cdns-images.dzcdn.net/images/cover/small.jpg",
					"cover_xl": "https://e-cdns-images.dzcdn.net/images/cover/xl.jpg"
				}
			}
		],
		"total": 1
	}`

	hc := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/search") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("q") != "kesariya" {
			t.Fatalf("unexpected query: %s", req.URL.Query().Get("q"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(mockJSON)),
		}, nil
	})

	client := New(providers.ClientOptions{HTTPClient: hc})
	tracks, err := client.SearchTracks(context.Background(), "kesariya", 10)
	if err != nil {
		t.Fatalf("SearchTracks failed: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	tr := tracks[0]
	if tr.ID != "dz:1827471677" {
		t.Errorf("expected ID dz:1827471677, got %s", tr.ID)
	}
	if tr.Title != "Kesariya" {
		t.Errorf("expected title Kesariya, got %s", tr.Title)
	}
	if len(tr.Artists) != 1 || tr.Artists[0].Name != "Pritam" {
		t.Errorf("unexpected artists: %+v", tr.Artists)
	}
	if tr.Album == nil || tr.Album.Name != "Brahmastra" {
		t.Errorf("unexpected album: %+v", tr.Album)
	}
	if tr.DurationSec != 268 {
		t.Errorf("expected duration 268, got %d", tr.DurationSec)
	}
	if !tr.Playable {
		t.Errorf("expected playable true for preview")
	}
}

func TestDeezerTrackAndMediaSources(t *testing.T) {
	mockTrackJSON := `{
		"id": 1827471677,
		"title": "Kesariya",
		"duration": 268,
		"preview": "https://cdns-preview-d.dzcdn.net/stream/sample.mp3",
		"artist": {"id": 1, "name": "Arijit Singh"},
		"album": {"id": 2, "title": "Brahmastra"},
		"isrc": "INS172200547"
	}`

	hc := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(mockTrackJSON)),
		}, nil
	})

	client := New(providers.ClientOptions{HTTPClient: hc})
	tr, err := client.Track(context.Background(), "dz:1827471677")
	if err != nil {
		t.Fatalf("Track failed: %v", err)
	}
	if tr.ProviderMeta["isrc"] != "INS172200547" {
		t.Errorf("expected isrc INS172200547, got %s", tr.ProviderMeta["isrc"])
	}

	sources, exp, err := client.MediaSources(context.Background(), "dz:1827471677")
	if err != nil {
		t.Fatalf("MediaSources failed: %v", err)
	}
	if len(sources) != 1 || sources[0].URL != "https://cdns-preview-d.dzcdn.net/stream/sample.mp3" {
		t.Errorf("unexpected sources: %+v", sources)
	}
	if exp.Before(time.Now()) {
		t.Errorf("expected expiry in future, got %v", exp)
	}
}

func TestDeezerErrorHandling(t *testing.T) {
	// Rate limit
	hcRateLimit := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})
	c := New(providers.ClientOptions{HTTPClient: hcRateLimit})
	_, err := c.SearchTracks(context.Background(), "test", 10)
	if catalog.KindOf(err) != catalog.KindRateLimited {
		t.Errorf("expected rate_limited, got: %v", err)
	}

	// 404
	hc404 := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})
	c404 := New(providers.ClientOptions{HTTPClient: hc404})
	_, err = c404.Track(context.Background(), "dz:9999999999")
	if catalog.KindOf(err) != catalog.KindNotFound {
		t.Errorf("expected not_found, got: %v", err)
	}
}
