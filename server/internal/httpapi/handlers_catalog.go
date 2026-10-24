package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"swar/internal/auth"
	"swar/internal/catalog"
	"swar/internal/providers/deepsearch"
	"swar/internal/search"
)

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	device := auth.NewDeviceID()
	if err := s.store.CreateDevice(r.Context(), device); err != nil {
		s.writeErr(w, r, err)
		return
	}
	token, exp := s.tokens.Issue(device)
	noStore(w)
	created(w, map[string]any{"deviceId": device, "token": token, "expiresAt": exp})
}

func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request, device string) {
	_ = s.store.TouchDevice(r.Context(), device)
	token, exp := s.tokens.Issue(device)
	noStore(w)
	ok(w, map[string]any{"deviceId": device, "token": token, "expiresAt": exp})
}

// handleDeleteDevice erases the device and all of its server-side data.
func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request, device string) {
	if err := s.store.DeleteDevice(r.Context(), device); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.devCache.Delete("dev:" + device)
	ok(w, map[string]any{"deleted": true})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	typ, valid := search.ParseType(r.URL.Query().Get("type"))
	if !valid {
		s.writeErr(w, r, catalog.Invalid("type must be all, songs, albums, artists or playlists"))
		return
	}
	page, err := intParam(r, "page", 0, 0, 100)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", 30, 1, 100)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	res, err := s.search.Search(r.Context(), r.URL.Query().Get("q"), typ, page, limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !res.Partial {
		publicCache(w, 5*time.Minute)
	}
	ok(w, res)
}

type deepSearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	// YouTube set to false asks for the catalog results only: the app ran
	// yt-dlp on the device itself, so the server does not start it (and is
	// not blocked by YouTube for it). Absent means true.
	YouTube *bool `json:"youtube,omitempty"`
}

// handleDeepSearch is POST so it can only run from an explicit user action
// (no prefetchers or crawlers following links).
func (s *Server) handleDeepSearch(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !s.cfg.DeepSearchEnabled {
		s.writeErr(w, r, catalog.Unavailable("Deep Search is not enabled on this server"))
		return
	}
	var req deepSearchRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	withYouTube := req.YouTube == nil || *req.YouTube
	res, err := s.deepSearchAll(r.Context(), req.Query, req.Limit, withYouTube)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	for i := range res {
		s.cat.Remember(res[i].Track)
	}
	ok(w, map[string]any{
		"query":   req.Query,
		"results": res,
		"notice":  "Deep Search looks on YouTube and in every music catalog that main search skips.",
	})
}

// deepSearchAll runs the YouTube search and the catalog sources main search
// leaves out (Deezer, iTunes, MusicBrainz when Spotify is on) side by side.
// YouTube results come first; either half failing still returns the other.
func (s *Server) deepSearchAll(ctx context.Context, query string, limit int, withYouTube bool) ([]deepsearch.Candidate, error) {
	var (
		wg            sync.WaitGroup
		yt            []deepsearch.Candidate
		ytErr         error
		catalogTracks []catalog.Track
		catalogErr    error
	)
	if withYouTube {
		wg.Add(1)
		go func() {
			defer wg.Done()
			yt, ytErr = s.deep.Search(ctx, query, limit)
		}()
	} else if _, _, err := deepsearch.ParseInput(query); err != nil {
		return nil, err // same validation as with YouTube
	}
	if q, err := catalog.NormalizeQuery(query); err == nil {
		if _, videoID, _ := deepsearch.ParseInput(q); videoID == "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				catalogTracks, catalogErr = s.cat.SearchSongsDeep(ctx, q, 15)
			}()
		}
	}
	wg.Wait()
	out := yt
	seen := map[string]bool{}
	for _, c := range yt {
		seen[strings.ToLower(c.Title)] = true
	}
	for _, t := range catalogTracks {
		key := strings.ToLower(t.Title)
		if seen[key] {
			continue // the same title is already listed
		}
		seen[key] = true
		out = append(out, deepsearch.Candidate{Track: t, Source: t.Provider, Confidence: "metadata_only"})
	}
	if out == nil {
		out = []deepsearch.Candidate{}
	}
	if len(out) == 0 {
		if ytErr != nil {
			return nil, ytErr
		}
		if catalogErr != nil {
			return nil, catalogErr
		}
	}
	return out, nil
}

// handleDeepSearchStatus reports whether Deep Search and yt-dlp work on
// this server, without paths or upstream error text.
func (s *Server) handleDeepSearchStatus(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	st := map[string]any{"enabled": s.cfg.DeepSearchEnabled}
	if s.cfg.DeepSearchEnabled && s.deep != nil {
		st["youtube"] = s.deep.Status(r.Context())
	}
	ok(w, st)
}

func (s *Server) handleTracks(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ids")
	if raw == "" {
		s.writeErr(w, r, catalog.Invalid("ids is required"))
		return
	}
	ids := strings.Split(raw, ",")
	ts, err := s.cat.Tracks(r.Context(), ids)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, ts)
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	t, err := s.cat.Track(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, t)
}

func (s *Server) handleRelated(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 30, 1, 100)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ts, err := s.cat.Related(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, 10*time.Minute)
	ok(w, ts)
}

func (s *Server) handleLyrics(w http.ResponseWriter, r *http.Request) {
	t, err := s.cat.Track(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !t.HasLyrics {
		s.writeErr(w, r, catalog.NotFound("lyrics are not available for this track"))
		return
	}
	l, err := s.cat.Lyrics(r.Context(), t.ID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, 24*time.Hour)
	ok(w, l)
}

func (s *Server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	a, err := s.cat.Album(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, a)
}

func artistOptions(r *http.Request) (catalog.ArtistOptions, error) {
	o := catalog.ArtistOptions{SortOrder: r.URL.Query().Get("sortOrder"), SortBy: catalog.SortBy(r.URL.Query().Get("sortBy"))}
	var err error
	if o.Page, err = intParam(r, "page", 0, 0, 1000); err != nil {
		return o, err
	}
	if o.SongCount, err = intParam(r, "songCount", 50, 0, 100); err != nil {
		return o, err
	}
	if o.AlbumCount, err = intParam(r, "albumCount", 50, 0, 100); err != nil {
		return o, err
	}
	switch o.SortBy {
	case "":
		o.SortBy = catalog.SortPopularity
	case catalog.SortPopularity, catalog.SortLatest, catalog.SortAlphabetical:
	default:
		return o, catalog.Invalid("sortBy must be popularity, latest or alphabetical")
	}
	switch o.SortOrder {
	case "":
		o.SortOrder = "asc"
	case "asc", "desc":
	default:
		return o, catalog.Invalid("sortOrder must be asc or desc")
	}
	return o, nil
}

func (s *Server) handleArtist(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	a, err := s.cat.Artist(r.Context(), r.PathValue("id"), o)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, a)
}

func (s *Server) handleArtistSongs(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := s.cat.ArtistSongs(r.Context(), r.PathValue("id"), o)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, p)
}

func (s *Server) handleArtistAlbums(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := s.cat.ArtistAlbums(r.Context(), r.PathValue("id"), o)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, p)
}

func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	page, err := intParam(r, "page", 0, 0, 100)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", 50, 1, 100)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := s.cat.Playlist(r.Context(), r.PathValue("id"), page, limit)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, p)
}

var (
	linkRe      = regexp.MustCompile(`^https?://(?:www\.)?jiosaavn\.com/(song|album|artist|featured|s/playlist)/[^/?#]+/([A-Za-z0-9_,\-]+)/?(?:[?#].*)?$`)
	linkTokenRe = regexp.MustCompile(`^[A-Za-z0-9_,\-]{1,64}$`)
)

// ParseSaavnLink extracts the entity kind and token from a JioSaavn share
// link. Only jiosaavn.com links are accepted, and the link is never
// fetched: the token is passed to the provider API.
func ParseSaavnLink(link string) (kind, token string, err error) {
	m := linkRe.FindStringSubmatch(strings.TrimSpace(link))
	if m == nil || !linkTokenRe.MatchString(m[2]) {
		return "", "", catalog.Invalid("link must be a jiosaavn.com song, album, artist or playlist link")
	}
	kind = m[1]
	if kind == "featured" || kind == "s/playlist" {
		kind = "playlist"
	}
	return kind, m[2], nil
}

func (s *Server) handleResolveLink(w http.ResponseWriter, r *http.Request) {
	kind, token, err := ParseSaavnLink(r.URL.Query().Get("link"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	var data any
	switch kind {
	case "song":
		data, err = s.cat.TrackByLink(r.Context(), token)
	case "album":
		data, err = s.cat.AlbumByLink(r.Context(), token)
	case "artist":
		data, err = s.cat.ArtistByLink(r.Context(), token, catalog.ArtistOptions{SongCount: 10, AlbumCount: 10, SortBy: catalog.SortPopularity, SortOrder: "asc"})
	case "playlist":
		data, err = s.cat.PlaylistByLink(r.Context(), token, 0, 50)
	}
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, time.Hour)
	ok(w, map[string]any{"type": kind, "result": data})
}

var languageRe = regexp.MustCompile(`^[a-z]{2,20}$`)

func language(r *http.Request) (string, error) {
	l := strings.ToLower(r.URL.Query().Get("lang"))
	if l == "" {
		return "hindi", nil
	}
	if !languageRe.MatchString(l) {
		return "", catalog.Invalid("lang must be a language name such as hindi or english")
	}
	return l, nil
}

func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request) {
	l, err := language(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	c, err := s.cat.Trending(r.Context(), l)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, 10*time.Minute)
	ok(w, map[string]any{"language": l, "collections": c, "verified": false})
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	l, err := language(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	c, err := s.cat.Browse(r.Context(), l)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	publicCache(w, 10*time.Minute)
	ok(w, map[string]any{"language": l, "collections": c, "verified": false})
}
