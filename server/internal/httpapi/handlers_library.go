package httpapi

import (
	"net/http"

	"swar/internal/catalog"
	"swar/internal/downloads"
	"swar/internal/repository"
)

func (s *Server) handleListReactions(kind string) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, device string) {
		noStore(w)
		limit, err := intParam(r, "limit", 200, 1, 1000)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		ts, err := s.lib.Reactions(r.Context(), device, repository.Reaction(kind), limit)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		ok(w, ts)
	}
}

func (s *Server) handleSetReaction(kind string, on bool) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, device string) {
		noStore(w)
		if err := s.lib.React(r.Context(), device, r.PathValue("trackId"), repository.Reaction(kind), on); err != nil {
			s.writeErr(w, r, err)
			return
		}
		ok(w, map[string]any{"trackId": r.PathValue("trackId"), kind: on})
	}
}

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	ps, err := s.lib.Playlists(r.Context(), device)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, ps)
}

type createPlaylistRequest struct {
	Name     string   `json:"name"`
	TrackIDs []string `json:"trackIds"`
}

func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var req createPlaylistRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := s.lib.CreatePlaylist(r.Context(), device, req.Name, req.TrackIDs)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	created(w, p)
}

func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	p, err := s.lib.Playlist(r.Context(), device, r.PathValue("id"), true)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, p)
}

type renamePlaylistRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenamePlaylist(w http.ResponseWriter, r *http.Request, device string) {
	var req renamePlaylistRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.lib.RenamePlaylist(r.Context(), device, r.PathValue("id"), req.Name); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.handleGetPlaylist(w, r, device)
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request, device string) {
	if err := s.lib.DeletePlaylist(r.Context(), device, r.PathValue("id")); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"deleted": true})
}

type setTracksRequest struct {
	TrackIDs []string `json:"trackIds"`
}

func (s *Server) handleSetPlaylistTracks(w http.ResponseWriter, r *http.Request, device string) {
	var req setTracksRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.lib.SetPlaylistTracks(r.Context(), device, r.PathValue("id"), req.TrackIDs); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.handleGetPlaylist(w, r, device)
}

type historyRequest struct {
	Events []repository.HistoryEvent `json:"events"`
}

func (s *Server) handleRecordHistory(w http.ResponseWriter, r *http.Request, device string) {
	var req historyRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.lib.RecordHistory(r.Context(), device, req.Events); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"recorded": len(req.Events)})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	limit, err := intParam(r, "limit", 50, 1, 500)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	h, err := s.lib.History(r.Context(), device, limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, h)
}

func (s *Server) handleClearHistory(w http.ResponseWriter, r *http.Request, device string) {
	if err := s.lib.ClearHistory(r.Context(), device); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"cleared": true})
}

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	limit, err := intParam(r, "limit", 20, 1, 100)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ts, err := s.lib.Recent(r.Context(), device, limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, ts)
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	p, err := s.lib.Preferences(r.Context(), device)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, p)
}

func (s *Server) handleSetPreferences(w http.ResponseWriter, r *http.Request, device string) {
	var prefs map[string]string
	if err := decode(r, &prefs); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.lib.SetPreferences(r.Context(), device, prefs); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.handleGetPreferences(w, r, device)
}

// ---- downloads ----

func (s *Server) handleDownloadEligibility(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	e, err := s.dl.Eligibility(r.Context(), r.PathValue("trackId"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, e)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	ds, err := s.dl.List(r.Context(), device)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ids := make([]string, len(ds))
	for i, d := range ds {
		ids[i] = d.TrackID
	}
	tracks, err := s.lib.Hydrate(r.Context(), ids)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	type item struct {
		repository.Download
		Track catalog.Track `json:"track"`
	}
	out := make([]item, len(ds))
	for i := range ds {
		out[i] = item{Download: ds[i], Track: tracks[i]}
	}
	ok(w, out)
}

func (s *Server) handleReportDownload(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var u downloads.Update
	if err := decode(r, &u); err != nil {
		s.writeErr(w, r, err)
		return
	}
	d, err := s.dl.Report(r.Context(), device, r.PathValue("trackId"), u)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, d)
}

func (s *Server) handleRemoveDownload(w http.ResponseWriter, r *http.Request, device string) {
	if err := s.dl.Remove(r.Context(), device, r.PathValue("trackId")); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"removed": true})
}
