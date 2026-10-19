package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
)

// Compatibility with the reference Saavn-Api (Node.js) routes under /api.
// Responses keep the old field names, raw Saavn IDs and the
// {success, data, credits} envelope. Differences are documented in
// docs/MIGRATION.md (notably: /api/search is assembled from the per-type
// searches, and downloadUrl is only filled when media URLs are enabled).

var compatCredits = map[string]any{
	"data":      "JioSaavn (Saavn Media Limited / Jio Platforms) — all music, metadata and media belong to JioSaavn and their respective owners",
	"reference": "https://github.com/sumitkolhe/jiosaavn-api",
	"creators": []map[string]string{
		{"name": "Anya", "github": "https://github.com/itz-Anya"},
		{"name": "Murali", "github": "https://github.com/Itz-Murali"},
	},
	"disclaimer": "Unofficial project. Not affiliated with or endorsed by JioSaavn.",
}

func compatOK(w http.ResponseWriter, data any, maxAge time.Duration) {
	publicCache(w, maxAge)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": data, "credits": compatCredits})
}

func compatFail(w http.ResponseWriter, err error) {
	status, _, msg := statusFor(err)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"success": false, "message": msg, "credits": compatCredits})
}

func (s *Server) saavnCompatRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/search", s.compatSearchAll)
	mux.HandleFunc("GET /api/search/songs", s.compatSearch("songs"))
	mux.HandleFunc("GET /api/search/albums", s.compatSearch("albums"))
	mux.HandleFunc("GET /api/search/artists", s.compatSearch("artists"))
	mux.HandleFunc("GET /api/search/playlists", s.compatSearch("playlists"))
	mux.HandleFunc("GET /api/songs", s.compatSongs)
	mux.HandleFunc("GET /api/songs/{id}", s.compatSongByID)
	mux.HandleFunc("GET /api/songs/{id}/suggestions", s.compatSuggestions)
	mux.HandleFunc("GET /api/albums", s.compatAlbum)
	mux.HandleFunc("GET /api/artists", s.compatArtist)
	mux.HandleFunc("GET /api/artists/{id}", s.compatArtistByID)
	mux.HandleFunc("GET /api/artists/{id}/songs", s.compatArtistSongs)
	mux.HandleFunc("GET /api/artists/{id}/albums", s.compatArtistAlbums)
	mux.HandleFunc("GET /api/playlists", s.compatPlaylist)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		compatFail(w, catalog.NotFound("Not found"))
	})
}

func rawID(id string) string {
	if _, pid, ok := catalog.ParseID(id); ok {
		return pid
	}
	return id
}

func saavnID(id string) string { return catalog.MakeID(catalog.ProviderSaavn, id) }

func validRaw(id string) bool {
	_, _, ok := catalog.ParseID(id)
	return ok && !strings.Contains(id, ":")
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func images(imgs []catalog.Image) []catalog.Image {
	if imgs == nil {
		return []catalog.Image{}
	}
	return imgs
}

func compatArtistMap(a catalog.ArtistRef) map[string]any {
	return map[string]any{"id": rawID(a.ID), "name": a.Name, "role": a.Role, "image": images(a.Image), "type": "artist", "url": a.URL}
}

func compatArtists(as []catalog.ArtistRef) map[string]any {
	primary, featured, all := []any{}, []any{}, []any{}
	for _, a := range as {
		m := compatArtistMap(a)
		switch a.Role {
		case "primary":
			primary = append(primary, m)
		case "featured":
			featured = append(featured, m)
		}
		all = append(all, m)
	}
	return map[string]any{"primary": primary, "featured": featured, "all": all}
}

type downloadLink struct {
	Quality string `json:"quality"`
	URL     string `json:"url"`
}

func compatSong(t catalog.Track, dl []downloadLink) map[string]any {
	year := any(nil)
	if t.Year > 0 {
		year = strconv.Itoa(t.Year)
	}
	album := map[string]any{"id": nil, "name": nil, "url": nil}
	if t.Album != nil {
		album = map[string]any{"id": nullStr(rawID(t.Album.ID)), "name": nullStr(t.Album.Name), "url": nullStr(t.Album.URL)}
	}
	if dl == nil {
		dl = []downloadLink{}
	}
	var duration any
	if t.DurationSec > 0 {
		duration = t.DurationSec
	}
	return map[string]any{
		"id": t.ProviderID, "name": t.Title, "type": "song", "year": year, "releaseDate": nullStr(t.ReleaseDate),
		"duration": duration, "label": nullStr(t.Label), "explicitContent": t.Explicit, "playCount": nullInt(t.PlayCount),
		"language": t.Language, "hasLyrics": t.HasLyrics, "lyricsId": nullStr(t.ProviderMeta["lyricsId"]), "url": t.URL,
		"copyright": nullStr(t.Copyright), "album": album, "artists": compatArtists(t.Artists),
		"image": images(t.Images), "downloadUrl": dl,
	}
}

// compatSongs converts tracks, filling downloadUrl only when the server
// is allowed to resolve media references (bounded concurrency).
func (s *Server) compatSongs(w http.ResponseWriter, r *http.Request) {
	ids := r.URL.Query().Get("ids")
	link := r.URL.Query().Get("link")
	var ts []catalog.Track
	var err error
	switch {
	case link != "":
		kind, token, perr := ParseSaavnLink(link)
		if perr != nil || kind != "song" {
			compatFail(w, catalog.Invalid("Either song IDs or link is required"))
			return
		}
		ts, err = s.cat.TrackByLink(r.Context(), token)
	case ids != "":
		parts := strings.Split(ids, ",")
		full := make([]string, 0, len(parts))
		for _, p := range parts {
			if !validRaw(strings.TrimSpace(p)) {
				compatFail(w, catalog.Invalid("invalid song id"))
				return
			}
			full = append(full, saavnID(strings.TrimSpace(p)))
		}
		ts, err = s.cat.Tracks(r.Context(), full)
		if err == nil && len(ts) == 0 {
			err = catalog.NotFound("song not found")
		}
	default:
		compatFail(w, catalog.Invalid("Either song IDs or link is required"))
		return
	}
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, s.compatSongList(r.Context(), ts), 24*time.Hour)
}

func (s *Server) compatSongList(ctx context.Context, ts []catalog.Track) []map[string]any {
	out := make([]map[string]any, len(ts))
	if !s.cfg.SaavnMediaURLs {
		for i, t := range ts {
			out[i] = compatSong(t, nil)
		}
		return out
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, t := range ts {
		wg.Add(1)
		go func(i int, t catalog.Track) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var dl []downloadLink
			if d, err := s.pb.Resolve(ctx, t.ID); err == nil && d.Available {
				for _, src := range d.Sources {
					dl = append(dl, downloadLink{Quality: src.Quality, URL: src.URL})
				}
			}
			out[i] = compatSong(t, dl)
		}(i, t)
	}
	wg.Wait()
	return out
}

func (s *Server) compatSongByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validRaw(id) {
		compatFail(w, catalog.Invalid("invalid song id"))
		return
	}
	t, err := s.cat.Track(r.Context(), saavnID(id))
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, s.compatSongList(r.Context(), []catalog.Track{*t}), 24*time.Hour)
}

func (s *Server) compatSuggestions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit, err := intParam(r, "limit", 10, 1, 50)
	if err != nil || !validRaw(id) {
		compatFail(w, catalog.Invalid("invalid request"))
		return
	}
	ts, err := s.cat.Related(r.Context(), saavnID(id), limit)
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, s.compatSongList(r.Context(), ts), 10*time.Minute)
}

func compatAlbum(a catalog.Album, songs []map[string]any) map[string]any {
	var year any
	if a.Year > 0 {
		year = a.Year
	}
	var songCount any
	if a.SongCount > 0 {
		songCount = a.SongCount
	}
	m := map[string]any{
		"id": a.ProviderID, "name": a.Name, "description": a.Description, "type": "album", "year": year,
		"playCount": nullInt(a.PlayCount), "language": a.Language, "explicitContent": a.Explicit, "url": a.URL,
		"songCount": songCount, "artists": compatArtists(a.Artists), "image": images(a.Images),
	}
	if songs != nil {
		m["songs"] = songs
	} else {
		m["songs"] = nil
	}
	return m
}

func (s *Server) compatAlbum(w http.ResponseWriter, r *http.Request) {
	var a *catalog.Album
	var err error
	if link := r.URL.Query().Get("link"); link != "" {
		kind, token, perr := ParseSaavnLink(link)
		if perr != nil || kind != "album" {
			compatFail(w, catalog.Invalid("Either album ID or link is required"))
			return
		}
		a, err = s.cat.AlbumByLink(r.Context(), token)
	} else if id := r.URL.Query().Get("id"); validRaw(id) {
		a, err = s.cat.Album(r.Context(), saavnID(id))
	} else {
		compatFail(w, catalog.Invalid("Either album ID or link is required"))
		return
	}
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, compatAlbum(*a, s.compatSongList(r.Context(), a.Tracks)), 24*time.Hour)
}

func (s *Server) compatArtistPayload(ctx context.Context, a *catalog.Artist) map[string]any {
	bio := any(nil)
	if len(a.Bio) > 0 {
		bio = a.Bio
	}
	albums := make([]map[string]any, 0, len(a.TopAlbums))
	for _, al := range a.TopAlbums {
		albums = append(albums, compatAlbum(al, nil))
	}
	similar := make([]map[string]any, 0, len(a.SimilarArtists))
	for _, sa := range a.SimilarArtists {
		similar = append(similar, map[string]any{"id": rawID(sa.ID), "name": sa.Name, "url": sa.URL, "image": images(sa.Image), "type": "artist"})
	}
	return map[string]any{
		"id": a.ProviderID, "name": a.Name, "url": a.URL, "type": "artist",
		"followerCount": nullInt(a.FollowerCount), "fanCount": nil, "isVerified": a.IsVerified,
		"dominantLanguage": nullStr(a.DominantLanguage), "dominantType": nullStr(a.DominantType), "bio": bio,
		"dob": nil, "fb": nil, "twitter": nil, "wiki": nil, "availableLanguages": nil, "isRadioPresent": a.IsRadioPresent,
		"image": images(a.Images), "topSongs": s.compatSongList(ctx, a.TopSongs), "topAlbums": albums,
		"singles": s.compatSongList(ctx, a.Singles), "similarArtists": similar,
	}
}

func (s *Server) compatArtist(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	if err != nil {
		compatFail(w, err)
		return
	}
	var a *catalog.Artist
	if link := r.URL.Query().Get("link"); link != "" {
		kind, token, perr := ParseSaavnLink(link)
		if perr != nil || kind != "artist" {
			compatFail(w, catalog.Invalid("Either artist ID or link is required"))
			return
		}
		a, err = s.cat.ArtistByLink(r.Context(), token, o)
	} else if id := r.URL.Query().Get("id"); validRaw(id) {
		a, err = s.cat.Artist(r.Context(), saavnID(id), o)
	} else {
		compatFail(w, catalog.Invalid("Either artist ID or link is required"))
		return
	}
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, s.compatArtistPayload(r.Context(), a), 24*time.Hour)
}

func (s *Server) compatArtistByID(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	id := r.PathValue("id")
	if err != nil || !validRaw(id) {
		compatFail(w, catalog.Invalid("invalid request"))
		return
	}
	a, err := s.cat.Artist(r.Context(), saavnID(id), o)
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, s.compatArtistPayload(r.Context(), a), 24*time.Hour)
}

func (s *Server) compatArtistSongs(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	id := r.PathValue("id")
	if err != nil || !validRaw(id) {
		compatFail(w, catalog.Invalid("invalid request"))
		return
	}
	p, err := s.cat.ArtistSongs(r.Context(), saavnID(id), o)
	if err != nil {
		compatFail(w, err)
		return
	}
	compatOK(w, map[string]any{"total": p.Total, "songs": s.compatSongList(r.Context(), p.Results)}, time.Hour)
}

func (s *Server) compatArtistAlbums(w http.ResponseWriter, r *http.Request) {
	o, err := artistOptions(r)
	id := r.PathValue("id")
	if err != nil || !validRaw(id) {
		compatFail(w, catalog.Invalid("invalid request"))
		return
	}
	p, err := s.cat.ArtistAlbums(r.Context(), saavnID(id), o)
	if err != nil {
		compatFail(w, err)
		return
	}
	albums := make([]map[string]any, 0, len(p.Results))
	for _, a := range p.Results {
		albums = append(albums, compatAlbum(a, s.compatSongList(r.Context(), a.Tracks)))
	}
	compatOK(w, map[string]any{"total": p.Total, "albums": albums}, time.Hour)
}

func (s *Server) compatPlaylist(w http.ResponseWriter, r *http.Request) {
	page, err1 := intParam(r, "page", 0, 0, 100)
	limit, err2 := intParam(r, "limit", 10, 1, 100)
	if err1 != nil || err2 != nil {
		compatFail(w, catalog.Invalid("invalid page or limit"))
		return
	}
	var p *catalog.Playlist
	var err error
	if link := r.URL.Query().Get("link"); link != "" {
		kind, token, perr := ParseSaavnLink(link)
		if perr != nil || kind != "playlist" {
			compatFail(w, catalog.Invalid("Either playlist ID or link is required"))
			return
		}
		p, err = s.cat.PlaylistByLink(r.Context(), token, page, limit)
	} else if id := r.URL.Query().Get("id"); validRaw(id) {
		p, err = s.cat.Playlist(r.Context(), saavnID(id), page, limit)
	} else {
		compatFail(w, catalog.Invalid("Either playlist ID or link is required"))
		return
	}
	if err != nil {
		compatFail(w, err)
		return
	}
	artists := make([]any, 0, len(p.Artists))
	for _, a := range p.Artists {
		artists = append(artists, compatArtistMap(a))
	}
	var songCount any
	if len(p.Tracks) > 0 {
		songCount = len(p.Tracks)
	}
	compatOK(w, map[string]any{
		"id": p.ProviderID, "name": p.Name, "description": p.Description, "type": "playlist", "year": nil,
		"playCount": nullInt(p.PlayCount), "language": p.Language, "explicitContent": p.Explicit, "url": p.URL,
		"songCount": songCount, "artists": artists, "image": images(p.Images), "songs": s.compatSongList(r.Context(), p.Tracks),
	}, 24*time.Hour)
}

func (s *Server) compatSearch(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, err := catalog.NormalizeQuery(r.URL.Query().Get("query"))
		if err != nil {
			compatFail(w, catalog.Invalid("query is required"))
			return
		}
		page, err1 := intParam(r, "page", 0, 0, 100)
		limit, err2 := intParam(r, "limit", 10, 1, 50)
		if err1 != nil || err2 != nil {
			compatFail(w, catalog.Invalid("invalid page or limit"))
			return
		}
		ctx := r.Context()
		var data any
		switch kind {
		case "songs":
			p, err := s.cat.SearchSongs(ctx, q, page, limit)
			if err != nil {
				compatFail(w, err)
				return
			}
			data = map[string]any{"total": p.Total, "start": p.Start, "results": s.compatSongList(ctx, p.Results)}
		case "albums":
			p, err := s.cat.SearchAlbums(ctx, q, page, limit)
			if err != nil {
				compatFail(w, err)
				return
			}
			res := make([]map[string]any, 0, len(p.Results))
			for _, a := range p.Results {
				m := compatAlbum(a, nil)
				delete(m, "songs")
				delete(m, "songCount")
				res = append(res, m)
			}
			data = map[string]any{"total": p.Total, "start": p.Start, "results": res}
		case "artists":
			p, err := s.cat.SearchArtists(ctx, q, page, limit)
			if err != nil {
				compatFail(w, err)
				return
			}
			res := make([]map[string]any, 0, len(p.Results))
			for _, a := range p.Results {
				res = append(res, compatArtistMap(a))
			}
			data = map[string]any{"total": p.Total, "start": p.Start, "results": res}
		case "playlists":
			p, err := s.cat.SearchPlaylists(ctx, q, page, limit)
			if err != nil {
				compatFail(w, err)
				return
			}
			res := make([]map[string]any, 0, len(p.Results))
			for _, pl := range p.Results {
				var sc any
				if pl.SongCount > 0 {
					sc = pl.SongCount
				}
				res = append(res, map[string]any{
					"id": pl.ProviderID, "name": pl.Name, "type": "playlist", "image": images(pl.Images), "url": pl.URL,
					"songCount": sc, "language": pl.Language, "explicitContent": pl.Explicit,
				})
			}
			data = map[string]any{"total": p.Total, "start": p.Start, "results": res}
		}
		compatOK(w, data, 30*time.Minute)
	}
}

// compatSearchAll rebuilds the reference /api/search shape from the
// per-type searches (5 results each).
func (s *Server) compatSearchAll(w http.ResponseWriter, r *http.Request) {
	res, err := s.search.Search(r.Context(), r.URL.Query().Get("query"), "all", 0, 5)
	if err != nil {
		compatFail(w, err)
		return
	}
	songs, albums, artists, playlists := []any{}, []any{}, []any{}, []any{}
	if res.Songs != nil {
		for _, t := range res.Songs.Results {
			album := ""
			if t.Album != nil {
				album = t.Album.Name
			}
			songs = append(songs, map[string]any{
				"id": t.ProviderID, "title": t.Title, "image": images(t.Images), "album": album, "url": t.URL, "type": "song",
				"description": album, "primaryArtists": t.ArtistNames(), "singers": t.ArtistNames(), "language": t.Language,
			})
		}
	}
	if res.Albums != nil {
		for _, a := range res.Albums.Results {
			names := make([]string, 0, len(a.Artists))
			for _, ar := range a.Artists {
				names = append(names, ar.Name)
			}
			year := ""
			if a.Year > 0 {
				year = strconv.Itoa(a.Year)
			}
			albums = append(albums, map[string]any{
				"id": a.ProviderID, "title": a.Name, "image": images(a.Images), "artist": strings.Join(names, ", "),
				"url": a.URL, "type": "album", "description": a.Description, "year": year, "songIds": "", "language": a.Language,
			})
		}
	}
	if res.Artists != nil {
		for i, a := range res.Artists.Results {
			artists = append(artists, map[string]any{
				"id": rawID(a.ID), "title": a.Name, "image": images(a.Image), "type": "artist", "description": a.Role, "position": i,
			})
		}
	}
	if res.Playlists != nil {
		for _, p := range res.Playlists.Results {
			playlists = append(playlists, map[string]any{
				"id": p.ProviderID, "title": p.Name, "image": images(p.Images), "url": p.URL, "type": "playlist",
				"language": p.Language, "description": p.Description,
			})
		}
	}
	compatOK(w, map[string]any{
		"topQuery":  map[string]any{"results": []any{}, "position": 0},
		"songs":     map[string]any{"results": songs, "position": 1},
		"albums":    map[string]any{"results": albums, "position": 2},
		"artists":   map[string]any{"results": artists, "position": 3},
		"playlists": map[string]any{"results": playlists, "position": 4},
	}, 30*time.Minute)
}
