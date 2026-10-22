// Package httpapi exposes Swar over HTTP and WebSockets.
//
// Route families:
//   - /v1/...            the documented API (docs/API.md)
//   - /health, /search, /track/{id}, /video/{id}, /related/{id}
//     compatibility routes used by the existing Flutter client
//   - /api/...           compatibility with the reference Saavn-Api routes
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"swar/internal/auth"
	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/config"
	"swar/internal/downloads"
	"swar/internal/jam"
	"swar/internal/library"
	"swar/internal/observability"
	"swar/internal/playback"
	"swar/internal/providers/deepsearch"
	"swar/internal/radio"
	"swar/internal/recommendations"
	"swar/internal/repository"
	"swar/internal/search"
)

type Deps struct {
	Config          *config.Config
	Log             *slog.Logger
	Metrics         *observability.Metrics
	Catalog         *catalog.Service
	Search          *search.Service
	DeepSearch      *deepsearch.Service
	Playback        *playback.Resolver
	Recommendations *recommendations.Service
	Radio           *radio.Manager
	Library         *library.Service
	Downloads       *downloads.Service
	Jam             *jam.Manager
	Store           *repository.Store
	Tokens          *auth.Tokens
}

type Server struct {
	cfg      *config.Config
	log      *slog.Logger
	metrics  *observability.Metrics
	cat      *catalog.Service
	search   *search.Service
	deep     *deepsearch.Service
	pb       *playback.Resolver
	rec      *recommendations.Service
	radio    *radio.Manager
	lib      *library.Service
	dl       *downloads.Service
	jam      *jam.Manager
	store    *repository.Store
	tokens   *auth.Tokens
	limiter  *rateLimiter
	devCache *cache.Cache
	status   statusCache
	handler  http.Handler
	ready    func(context.Context) error
}

func New(d Deps) *Server {
	s := &Server{
		cfg: d.Config, log: d.Log, metrics: d.Metrics, cat: d.Catalog, search: d.Search, deep: d.DeepSearch,
		pb: d.Playback, rec: d.Recommendations, radio: d.Radio, lib: d.Library, dl: d.Downloads, jam: d.Jam,
		store: d.Store, tokens: d.Tokens,
		limiter:  newRateLimiter(d.Config.RateLimitRPS, d.Config.RateLimitBurst),
		devCache: cache.New(10000),
	}
	s.ready = func(ctx context.Context) error { return s.store.Ping(ctx) }
	if err := s.reloadStatus(context.Background()); err != nil {
		s.log.Error("load maintenance and popup state", "err", err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	s.handler = s.base(s.compress(s.cors(s.appState(s.rateLimit(s.limits(s.authenticate(mux)))))))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

func (s *Server) routes(mux *http.ServeMux) {
	// Health and metrics.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	// App status (maintenance, popup) and its admin API.
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/admin/maintenance", s.requireAdmin(s.handleAdminGetMaintenance))
	mux.HandleFunc("PUT /v1/admin/maintenance", s.requireAdmin(s.handleAdminSetMaintenance))
	mux.HandleFunc("GET /v1/admin/popups", s.requireAdmin(s.handleAdminListPopups))
	mux.HandleFunc("POST /v1/admin/popups", s.requireAdmin(s.handleAdminCreatePopup))
	mux.HandleFunc("GET /v1/admin/popups/active", s.requireAdmin(s.handleAdminGetActivePopup))
	mux.HandleFunc("PUT /v1/admin/popups/active", s.requireAdmin(s.handleAdminActivatePopup))
	mux.HandleFunc("DELETE /v1/admin/popups/active", s.requireAdmin(s.handleAdminDeactivatePopup))
	mux.HandleFunc("GET /v1/admin/popups/{id}", s.requireAdmin(s.handleAdminGetPopup))
	mux.HandleFunc("PUT /v1/admin/popups/{id}", s.requireAdmin(s.handleAdminUpdatePopup))
	mux.HandleFunc("DELETE /v1/admin/popups/{id}", s.requireAdmin(s.handleAdminDeletePopup))

	// Device identity.
	mux.HandleFunc("POST /v1/auth/device", s.handleRegisterDevice)
	mux.HandleFunc("POST /v1/auth/refresh", s.requireDevice(s.handleRefreshToken))
	mux.HandleFunc("DELETE /v1/auth/device", s.requireDevice(s.handleDeleteDevice))

	// Catalog.
	mux.HandleFunc("GET /v1/search", s.handleSearch)
	mux.HandleFunc("POST /v1/search/deep", s.handleDeepSearch)
	mux.HandleFunc("GET /v1/tracks", s.handleTracks)
	mux.HandleFunc("GET /v1/tracks/{id}", s.handleTrack)
	mux.HandleFunc("GET /v1/tracks/{id}/related", s.handleRelated)
	mux.HandleFunc("GET /v1/tracks/{id}/lyrics", s.handleLyrics)
	mux.HandleFunc("GET /v1/albums/{id}", s.handleAlbum)
	mux.HandleFunc("GET /v1/artists/{id}", s.handleArtist)
	mux.HandleFunc("GET /v1/artists/{id}/songs", s.handleArtistSongs)
	mux.HandleFunc("GET /v1/artists/{id}/albums", s.handleArtistAlbums)
	mux.HandleFunc("GET /v1/playlists/{id}", s.handlePlaylist)
	mux.HandleFunc("GET /v1/resolve", s.handleResolveLink)
	mux.HandleFunc("GET /v1/browse/trending", s.handleTrending)
	mux.HandleFunc("GET /v1/browse/modules", s.handleBrowse)

	// Playback, queue, recommendations, radio.
	mux.HandleFunc("GET /v1/playback/{id}", s.handlePlayback)
	mux.HandleFunc("POST /v1/playback/{id}/invalidate", s.handlePlaybackInvalidate)
	mux.HandleFunc("GET /v1/playback/{id}/audio", s.handlePlaybackAudio)
	mux.HandleFunc("POST /v1/playback/prefetch", s.handlePrefetch)
	mux.HandleFunc("GET /v1/queue", s.requireDevice(s.handleGetQueue))
	mux.HandleFunc("PUT /v1/queue", s.requireDevice(s.handleReplaceQueue))
	mux.HandleFunc("POST /v1/queue/ops", s.requireDevice(s.handleQueueOp))
	mux.HandleFunc("POST /v1/recommendations/next", s.handleRecommendations)
	mux.HandleFunc("POST /v1/radio", s.handleRadioStart)
	mux.HandleFunc("GET /v1/radio/{id}", s.handleRadioGet)
	mux.HandleFunc("POST /v1/radio/{id}/next", s.handleRadioNext)
	mux.HandleFunc("POST /v1/radio/{id}/feedback", s.handleRadioFeedback)
	mux.HandleFunc("DELETE /v1/radio/{id}", s.handleRadioStop)

	// Library.
	mux.HandleFunc("GET /v1/library/likes", s.requireDevice(s.handleListReactions("likes")))
	mux.HandleFunc("PUT /v1/library/likes/{trackId}", s.requireDevice(s.handleSetReaction("likes", true)))
	mux.HandleFunc("DELETE /v1/library/likes/{trackId}", s.requireDevice(s.handleSetReaction("likes", false)))
	mux.HandleFunc("GET /v1/library/dislikes", s.requireDevice(s.handleListReactions("dislikes")))
	mux.HandleFunc("PUT /v1/library/dislikes/{trackId}", s.requireDevice(s.handleSetReaction("dislikes", true)))
	mux.HandleFunc("DELETE /v1/library/dislikes/{trackId}", s.requireDevice(s.handleSetReaction("dislikes", false)))
	mux.HandleFunc("GET /v1/library/playlists", s.requireDevice(s.handleListPlaylists))
	mux.HandleFunc("POST /v1/library/playlists", s.requireDevice(s.handleCreatePlaylist))
	mux.HandleFunc("GET /v1/library/playlists/{id}", s.requireDevice(s.handleGetPlaylist))
	mux.HandleFunc("PATCH /v1/library/playlists/{id}", s.requireDevice(s.handleRenamePlaylist))
	mux.HandleFunc("DELETE /v1/library/playlists/{id}", s.requireDevice(s.handleDeletePlaylist))
	mux.HandleFunc("PUT /v1/library/playlists/{id}/tracks", s.requireDevice(s.handleSetPlaylistTracks))
	mux.HandleFunc("POST /v1/library/history", s.requireDevice(s.handleRecordHistory))
	mux.HandleFunc("GET /v1/library/history", s.requireDevice(s.handleHistory))
	mux.HandleFunc("DELETE /v1/library/history", s.requireDevice(s.handleClearHistory))
	mux.HandleFunc("GET /v1/library/recent", s.requireDevice(s.handleRecent))
	mux.HandleFunc("GET /v1/library/preferences", s.requireDevice(s.handleGetPreferences))
	mux.HandleFunc("PUT /v1/library/preferences", s.requireDevice(s.handleSetPreferences))

	// Downloads (eligibility and status only; never audio).
	mux.HandleFunc("GET /v1/downloads/eligibility/{trackId}", s.handleDownloadEligibility)
	mux.HandleFunc("GET /v1/downloads", s.requireDevice(s.handleListDownloads))
	mux.HandleFunc("PUT /v1/downloads/{trackId}", s.requireDevice(s.handleReportDownload))
	mux.HandleFunc("DELETE /v1/downloads/{trackId}", s.requireDevice(s.handleRemoveDownload))

	// Swar Jam.
	mux.HandleFunc("POST /v1/jams", s.requireDevice(s.handleJamCreate))
	mux.HandleFunc("POST /v1/jams/join", s.requireDevice(s.handleJamJoin))
	mux.HandleFunc("GET /v1/jams/{id}", s.requireDevice(s.handleJamGet))
	mux.HandleFunc("POST /v1/jams/{id}/leave", s.requireDevice(s.handleJamLeave))
	mux.HandleFunc("POST /v1/jams/{id}/end", s.requireDevice(s.handleJamEnd))
	mux.HandleFunc("POST /v1/jams/{id}/commands", s.requireDevice(s.handleJamCommand))
	mux.HandleFunc("GET /v1/jams/{id}/participants", s.requireDevice(s.handleJamParticipants))
	mux.HandleFunc("DELETE /v1/jams/{id}/participants/{pid}", s.requireDevice(s.handleJamRemoveParticipant))
	mux.HandleFunc("GET /v1/jams/{id}/ws", s.requireDevice(s.handleJamWS))

	// Compatibility with the existing Flutter client.
	mux.HandleFunc("GET /health", s.handleLegacyHealth)
	mux.HandleFunc("GET /search", s.handleLegacySearch)
	mux.HandleFunc("GET /track/{id}", s.handleLegacyTrack)
	mux.HandleFunc("GET /video/{id}", s.handleLegacyVideo)
	mux.HandleFunc("GET /related/{id}", s.handleLegacyRelated)

	// Compatibility with the reference Saavn-Api routes.
	s.saavnCompatRoutes(mux)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "Not found")
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	ok(w, map[string]any{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	checks := map[string]string{"database": "ok"}
	status := http.StatusOK
	if err := s.ready(ctx); err != nil {
		checks["database"] = "unavailable"
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"success": status == http.StatusOK, "data": map[string]any{"checks": checks}})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	noStore(w)
	s.metrics.WritePrometheus(w)
}

// Background runs periodic housekeeping until ctx is done.
func (s *Server) Background(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.jam.GC()
			s.radio.GC()
			s.limiter.gc()
		}
	}
}
