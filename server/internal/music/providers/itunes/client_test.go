package itunes

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

func TestITunesSearchTracks(t *testing.T) {
	mockJSON := `{
		"resultCount": 1,
		"results": [
			{
				"wrapperType": "track",
				"kind": "song",
				"artistId": 4443585,
				"collectionId": 1634567890,
				"trackId": 1634567895,
				"artistName": "Pritam & Arijit Singh",
				"collectionName": "Brahmastra",
				"trackName": "Kesariya",
				"previewUrl": "https://audio-ssl.itunes.apple.com/stream.m4a",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/image/thumb/100x100bb.jpg",
				"releaseDate": "2022-07-17T07:00:00Z",
				"trackTimeMillis": 268120,
				"primaryGenreName": "Bollywood",
				"trackExplicitness": "notExplicit"
			}
		]
	}`

	hc := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/search") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("term") != "kesariya" {
			t.Fatalf("unexpected query: %s", req.URL.Query().Get("term"))
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
	if tr.ID != "it:1634567895" {
		t.Errorf("expected ID it:1634567895, got %s", tr.ID)
	}
	if tr.Title != "Kesariya" {
		t.Errorf("expected title Kesariya, got %s", tr.Title)
	}
	if tr.DurationSec != 268 {
		t.Errorf("expected duration 268, got %d", tr.DurationSec)
	}
	if tr.Genre != "Bollywood" {
		t.Errorf("expected genre Bollywood, got %s", tr.Genre)
	}
	if !tr.Playable {
		t.Errorf("expected playable true")
	}

	// Verify multi-resolution artwork scaling
	if len(tr.Images) != 4 {
		t.Fatalf("expected 4 image resolutions, got %d", len(tr.Images))
	}
	best := catalog.BestImage(tr.Images)
	if !strings.Contains(best, "1000x1000bb.jpg") {
		t.Errorf("expected best image to be 1000x1000, got %s", best)
	}
}

func TestITunesTrackAndMediaSources(t *testing.T) {
	mockTrackJSON := `{
		"resultCount": 1,
		"results": [
			{
				"wrapperType": "track",
				"trackId": 1634567895,
				"trackName": "Kesariya",
				"previewUrl": "https://audio-ssl.itunes.apple.com/stream.m4a",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/image/thumb/100x100bb.jpg",
				"trackTimeMillis": 268120
			}
		]
	}`

	hc := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(mockTrackJSON)),
		}, nil
	})

	client := New(providers.ClientOptions{HTTPClient: hc})
	tr, err := client.Track(context.Background(), "it:1634567895")
	if err != nil {
		t.Fatalf("Track failed: %v", err)
	}
	if tr.ProviderMeta["preview_url"] != "https://audio-ssl.itunes.apple.com/stream.m4a" {
		t.Errorf("unexpected preview_url: %s", tr.ProviderMeta["preview_url"])
	}

	sources, exp, err := client.MediaSources(context.Background(), "it:1634567895")
	if err != nil {
		t.Fatalf("MediaSources failed: %v", err)
	}
	if len(sources) != 1 || sources[0].URL != "https://audio-ssl.itunes.apple.com/stream.m4a" {
		t.Errorf("unexpected sources: %+v", sources)
	}
	if sources[0].Bitrate != 256 {
		t.Errorf("expected bitrate 256, got %d", sources[0].Bitrate)
	}
	if exp.Before(time.Now()) {
		t.Errorf("expected future expiry")
	}
}

func TestITunesErrorHandling(t *testing.T) {
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

	// Not found
	hc404 := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"resultCount":0,"results":[]}`)),
		}, nil
	})
	c404 := New(providers.ClientOptions{HTTPClient: hc404})
	_, err = c404.Track(context.Background(), "it:99999999")
	if catalog.KindOf(err) != catalog.KindNotFound {
		t.Errorf("expected not_found, got: %v", err)
	}
}
