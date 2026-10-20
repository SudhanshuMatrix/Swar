package httpapi

import (
	"net/http"
	"time"

	"swar/internal/catalog"
	"swar/internal/recommendations"
)

// The original Flutter client (lib/services/api_service.dart) calls these
// routes and reads these exact fields:
//
//	GET /search?q=&limit=  -> {"results":[{id,title,artist,thumbnail,duration}]}
//	GET /track/{id}        -> {id,title,artist,thumbnail,duration,audio_url}
//	GET /video/{id}        -> {..., video_url, audio_url}
//	GET /related/{id}      -> {"results":[...]}
//
// They are kept so the current app keeps working. IDs are now Swar
// internal IDs ("saavn:abc123", "yt:dQw4w9WgXcQ"); bare 11-character IDs
// are still accepted as YouTube IDs.

type legacyTrack struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Thumbnail string `json:"thumbnail"`
	Duration  int    `json:"duration"`
	// Additive fields; old clients ignore them.
	Album        string `json:"album,omitempty"`
	Provider     string `json:"provider"`
	Playable     bool   `json:"playable"`
	Downloadable bool   `json:"downloadable"`
	Language     string `json:"language,omitempty"`
}

func toLegacy(t catalog.Track) legacyTrack {
	lt := legacyTrack{
		ID: t.ID, Title: t.Title, Artist: t.ArtistNames(), Thumbnail: catalog.BestImage(t.Images),
		Duration: t.DurationSec, Provider: t.Provider, Playable: t.Playable, Downloadable: t.Download.Eligible,
		Language: t.Language,
	}
	if t.Album != nil {
		lt.Album = t.Album.Name
	}
	if lt.Title == "" {
		lt.Title = "Unknown title"
	}
	return lt
}

func legacyList(ts []catalog.Track) []legacyTrack {
	out := make([]legacyTrack, 0, len(ts))
	for _, t := range ts {
		out = append(out, toLegacy(t))
	}
	return out
}

func legacyError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := statusFor(err)
	writeJSON(w, status, map[string]any{"error": msg, "code": code})
}

func (s *Server) handleLegacyHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleLegacySearch(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 25, 1, 50)
	if err != nil {
		legacyError(w, r, err)
		return
	}
	q, err := catalog.NormalizeQuery(r.URL.Query().Get("q"))
	if err != nil {
		legacyError(w, r, err)
		return
	}
	p, err := s.cat.SearchSongs(r.Context(), q, 0, limit)
	if err != nil {
		legacyError(w, r, err)
		return
	}
	publicCache(w, 5*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"results": legacyList(p.Results)})
}

func (s *Server) legacyResolve(w http.ResponseWriter, r *http.Request, wantVideo bool) {
	noStore(w)
	id := r.PathValue("id")
	t, err := s.cat.Track(r.Context(), id)
	if err != nil {
		legacyError(w, r, err)
		return
	}
	d, err := s.pb.Resolve(r.Context(), t.ID)
	if err != nil {
		legacyError(w, r, err)
		return
	}
	if !d.Available {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": d.Reason, "code": "unavailable"})
		return
	}
	var audio, video string
	for _, src := range d.Sources {
		switch src.Kind {
		case "audio":
			// Prefer 160kbps for streaming when offered (good quality,
			// moderate data); otherwise the first audio source.
			if audio == "" || src.Quality == "160kbps" {
				audio = src.URL
			}
		case "video":
			if video == "" {
				video = src.URL
			}
		}
	}
	if audio == "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "no playable audio source", "code": "unavailable"})
		return
	}
	if needsRelay(t) {
		// Relative: the client joins it to its own server address.
		audio = relayPath(t.ID)
	}
	resp := map[string]any{
		"id": t.ID, "title": t.Title, "artist": t.ArtistNames(), "thumbnail": catalog.BestImage(t.Images),
		"duration": t.DurationSec, "audio_url": audio, "expires_at": d.ExpiresAt,
	}
	if wantVideo {
		if video == "" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "this track has no video", "code": "not_found"})
			return
		}
		resp["video_url"] = video
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLegacyTrack(w http.ResponseWriter, r *http.Request) {
	s.legacyResolve(w, r, false)
}
func (s *Server) handleLegacyVideo(w http.ResponseWriter, r *http.Request) {
	s.legacyResolve(w, r, true)
}

func (s *Server) handleLegacyRelated(w http.ResponseWriter, r *http.Request) {
	res, err := s.rec.Next(r.Context(), recommendations.Request{SeedTrackID: r.PathValue("id"), Limit: 15})
	if err != nil {
		legacyError(w, r, err)
		return
	}
	ts := make([]catalog.Track, 0, len(res.Results))
	for _, rr := range res.Results {
		ts = append(ts, rr.Track)
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"results": legacyList(ts)})
}
