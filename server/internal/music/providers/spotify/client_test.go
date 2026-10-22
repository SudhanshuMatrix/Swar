package spotify

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

func TestSpotifyDisabledWhenNotConfigured(t *testing.T) {
	c := New(Options{})
	if c.Enabled() {
		t.Errorf("expected disabled without credentials")
	}
	tracks, err := c.SearchTracks(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(tracks) != 0 {
		t.Errorf("expected 0 tracks when disabled, got %d", len(tracks))
	}
	_, err = c.Track(context.Background(), "sp:123")
	if catalog.KindOf(err) != catalog.KindUnavailable {
		t.Errorf("expected KindUnavailable, got %v", err)
	}
}

func TestSpotifyAuthAndSearch(t *testing.T) {
	authJSON := `{"access_token": "test-token-123", "token_type": "Bearer", "expires_in": 3600}`
	searchJSON := `{
		"tracks": {
			"items": [
				{
					"id": "4cOdK2wGLETKBW3PvgPWqT",
					"name": "Kesariya",
					"duration_ms": 268000,
					"popularity": 85,
					"preview_url": "https://p.scdn.co/mp3-preview/sample.mp3",
					"external_ids": {"isrc": "INS172200547"},
					"artists": [{"id": "4YRxDV8wJFPHPTeXepOstw", "name": "Arijit Singh"}],
					"album": {
						"id": "1A2B3C",
						"name": "Brahmastra",
						"release_date": "2022-07-17",
						"images": [{"url": "https://i.scdn.co/image/640", "width": 640, "height": 640}]
					}
				}
			]
		}
	}`

	hc := mockHTTPClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/api/token") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(authJSON)),
			}, nil
		}
		if strings.Contains(req.URL.Path, "/search") {
			if req.Header.Get("Authorization") != "Bearer test-token-123" {
				t.Fatalf("expected bearer token, got: %s", req.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(searchJSON)),
			}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
	})

	client := New(Options{
		ClientOptions: providers.ClientOptions{HTTPClient: hc},
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		AuthURL:       "https://accounts.spotify.com/api/token",
	})

	if !client.Enabled() {
		t.Fatalf("expected client to be enabled")
	}

	tracks, err := client.SearchTracks(context.Background(), "kesariya", 5)
	if err != nil {
		t.Fatalf("SearchTracks failed: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	tr := tracks[0]
	if tr.ID != "sp:4cOdK2wGLETKBW3PvgPWqT" {
		t.Errorf("unexpected ID: %s", tr.ID)
	}
	if tr.Title != "Kesariya" {
		t.Errorf("unexpected title: %s", tr.Title)
	}
	if tr.DurationSec != 268 {
		t.Errorf("expected 268s, got %d", tr.DurationSec)
	}
	if tr.ProviderMeta["isrc"] != "INS172200547" {
		t.Errorf("unexpected isrc: %s", tr.ProviderMeta["isrc"])
	}
}
