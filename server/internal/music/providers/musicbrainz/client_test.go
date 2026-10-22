package musicbrainz

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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

func TestMusicBrainzSearchTracks(t *testing.T) {
	mockJSON := `{
		"count": 1,
		"recordings": [
			{
				"id": "c42d3d9e-1082-4148-895c-78332d7870a6",
				"title": "Kesariya",
				"length": 268000,
				"artist-credit": [
					{
						"name": "Pritam",
						"artist": {
							"id": "b10bbbfc-cf9e-42e0-be17-e2c3e1d52a73",
							"name": "Pritam"
						}
					}
				],
				"releases": [
					{
						"id": "678e8b61-2679-450f-96a8-086cb4f86d8a",
						"title": "Brahmastra",
						"date": "2022-07-17",
						"country": "IN",
						"track-count": 12
					}
				],
				"isrcs": ["INS172200547"],
				"tags": [{"count": 5, "name": "bollywood"}]
			}
		]
	}`

	hc := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/recording/") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("query") != "kesariya" {
			t.Fatalf("unexpected query: %s", req.URL.Query().Get("query"))
		}
		if !strings.Contains(req.Header.Get("User-Agent"), "Swar") {
			t.Errorf("expected User-Agent header to contain Swar, got %s", req.Header.Get("User-Agent"))
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
	if tr.ID != "mb:c42d3d9e-1082-4148-895c-78332d7870a6" {
		t.Errorf("unexpected ID: %s", tr.ID)
	}
	if tr.Title != "Kesariya" {
		t.Errorf("unexpected title: %s", tr.Title)
	}
	if tr.DurationSec != 268 {
		t.Errorf("expected 268 sec, got %d", tr.DurationSec)
	}
	if tr.ProviderMeta["isrc"] != "INS172200547" {
		t.Errorf("expected isrc INS172200547, got %s", tr.ProviderMeta["isrc"])
	}
	if tr.Playable {
		t.Errorf("musicbrainz should be metadata-only (playable=false)")
	}
}

func TestMusicBrainzMediaSources(t *testing.T) {
	client := New(providers.ClientOptions{HTTPClient: mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
	})})

	_, _, err := client.MediaSources(context.Background(), "mb:123")
	if catalog.KindOf(err) != catalog.KindUnavailable {
		t.Errorf("expected KindUnavailable for media sources, got %v", err)
	}
}

func TestMusicBrainzErrorHandling(t *testing.T) {
	hc503 := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})
	c := New(providers.ClientOptions{HTTPClient: hc503})
	_, err := c.SearchTracks(context.Background(), "test", 10)
	if catalog.KindOf(err) != catalog.KindRateLimited {
		t.Errorf("expected rate_limited, got: %v", err)
	}
}
