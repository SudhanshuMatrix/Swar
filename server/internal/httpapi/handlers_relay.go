package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"swar/internal/catalog"
	"swar/internal/playback"
)

// YouTube stream URLs are signed for the IP address that asked for them,
// which is this server, so a phone or desktop gets 403 when it fetches one
// directly. Deep Search audio is therefore relayed through the server. Only
// googlevideo.com hosts are fetched, so the relay cannot be pointed
// anywhere else.

var relayClient = &http.Client{
	// No overall timeout: a song streams for minutes. The request context
	// ends the transfer when the listener goes away.
	Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 20 * time.Second},
}

// relayPath is the audio_url handed to clients for relayed tracks.
func relayPath(trackID string) string {
	return "/v1/playback/" + url.PathEscape(trackID) + "/audio"
}

func needsRelay(t *catalog.Track, d ...*playback.Descriptor) bool {
	if t != nil && t.Provider == catalog.ProviderYouTube {
		return true
	}
	if len(d) > 0 && d[0] != nil {
		for _, src := range d[0].Sources {
			if strings.Contains(src.URL, ".googlevideo.com") {
				return true
			}
		}
	}
	return false
}

func isRelayRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/playback/") && strings.HasSuffix(r.URL.Path, "/audio")
}

func (s *Server) handlePlaybackAudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.cat.Track(ctx, r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	d, err := s.pb.Resolve(ctx, t.ID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !needsRelay(t, d) {
		s.writeErr(w, r, catalog.Invalid("this track streams directly"))
		return
	}
	for attempt := 0; attempt < 2; attempt++ {
		src, err := s.relaySource(ctx, t.ID)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		resp, err := fetchRelay(ctx, src, r.Header.Get("Range"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "could not reach the audio source", "code": "upstream"})
			return
		}
		if resp.StatusCode == http.StatusForbidden && attempt == 0 {
			// The signed URL expired: resolve a fresh one and try again.
			resp.Body.Close()
			s.pb.Invalidate(t.ID)
			continue
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]any{"error": "the audio source refused the request", "code": "upstream"})
}

func (s *Server) relaySource(ctx context.Context, id string) (string, error) {
	d, err := s.pb.Resolve(ctx, id)
	if err != nil {
		return "", err
	}
	if !d.Available {
		return "", catalog.Unavailable(d.Reason)
	}
	for _, src := range d.Sources {
		if src.Kind == "audio" {
			return src.URL, nil
		}
	}
	return "", catalog.Unavailable("no playable audio source")
}

func fetchRelay(ctx context.Context, raw, rangeHeader string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".googlevideo.com") {
		return nil, catalog.Unavailable("unexpected audio host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")
	return relayClient.Do(req)
}
