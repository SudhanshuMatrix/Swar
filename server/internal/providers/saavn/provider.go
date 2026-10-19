package saavn

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"swar/internal/catalog"
)

func pageParams(q string, page, limit int) url.Values {
	// Saavn pages are 1-based; Swar's public API keeps the reference
	// project's 0-based "page", so 0 and 1 both mean the first page.
	if page < 1 {
		page = 1
	}
	return url.Values{"q": {q}, "p": {strconv.Itoa(page)}, "n": {strconv.Itoa(limit)}}
}

func (c *Client) SearchSongs(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Track], error) {
	var r rawPaged[rawSong]
	if err := c.get(ctx, callSearchSongs, pageParams(q, page, limit), ctxWeb, &r); err != nil {
		return nil, err
	}
	res := c.tracks(r.Results)
	if len(res) > limit {
		res = res[:limit]
	}
	return &catalog.Page[catalog.Track]{Total: r.Total.Int(), Start: r.Start.Int(), Page: page, Limit: limit, Results: res}, nil
}

func (c *Client) SearchAlbums(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Album], error) {
	var r rawPaged[rawAlbum]
	if err := c.get(ctx, callSearchAlbums, pageParams(q, page, limit), ctxWeb, &r); err != nil {
		return nil, err
	}
	res := make([]catalog.Album, 0, len(r.Results))
	for _, a := range r.Results {
		if a.ID != "" && len(res) < limit {
			res = append(res, c.album(a))
		}
	}
	return &catalog.Page[catalog.Album]{Total: r.Total.Int(), Start: r.Start.Int(), Page: page, Limit: limit, Results: res}, nil
}

func (c *Client) SearchArtists(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.ArtistRef], error) {
	var r rawPaged[rawArtistMapEntry]
	if err := c.get(ctx, callSearchArtists, pageParams(q, page, limit), ctxWeb, &r); err != nil {
		return nil, err
	}
	res := make([]catalog.ArtistRef, 0, len(r.Results))
	for _, a := range r.Results {
		if a.ID != "" && len(res) < limit {
			res = append(res, artistRef(a, ""))
		}
	}
	return &catalog.Page[catalog.ArtistRef]{Total: r.Total.Int(), Start: r.Start.Int(), Page: page, Limit: limit, Results: res}, nil
}

func (c *Client) SearchPlaylists(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Playlist], error) {
	var r rawPaged[rawPlaylist]
	if err := c.get(ctx, callSearchPlaylists, pageParams(q, page, limit), ctxWeb, &r); err != nil {
		return nil, err
	}
	res := make([]catalog.Playlist, 0, len(r.Results))
	for _, p := range r.Results {
		if p.ID != "" && len(res) < limit {
			res = append(res, c.playlist(p))
		}
	}
	return &catalog.Page[catalog.Playlist]{Total: r.Total.Int(), Start: r.Start.Int(), Page: page, Limit: limit, Results: res}, nil
}

func (c *Client) Tracks(ctx context.Context, ids []string) ([]catalog.Track, error) {
	var r rawSongsResponse
	if err := c.get(ctx, callSongDetails, url.Values{"pids": {strings.Join(ids, ",")}}, ctxWeb, &r); err != nil {
		return nil, err
	}
	ts := c.tracks(r.Songs)
	if len(ts) == 0 {
		return nil, catalog.NotFound("track not found")
	}
	return ts, nil
}

func (c *Client) TrackByLink(ctx context.Context, token string) ([]catalog.Track, error) {
	var r rawSongsResponse
	if err := c.get(ctx, callWebAPI, url.Values{"token": {token}, "type": {"song"}}, ctxWeb, &r); err != nil {
		return nil, err
	}
	ts := c.tracks(r.Songs)
	if len(ts) == 0 {
		return nil, catalog.NotFound("track not found")
	}
	return ts, nil
}

// Related uses Saavn's own song station (webradio), which is a genuine
// provider capability used by the reference implementation.
func (c *Client) Related(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	stationID, err := c.createStation(ctx, id)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	params := url.Values{"stationid": {stationID}, "k": {strconv.Itoa(limit)}}
	if err := c.get(ctx, callStationSongs, params, ctxAndroid, &raw); err != nil {
		return nil, err
	}
	// The response is an object keyed "0", "1", ... plus "stationid".
	type entry struct {
		Song obj[rawSong] `json:"song"`
	}
	keys := make([]int, 0, len(raw))
	for k := range raw {
		if n, err := strconv.Atoi(k); err == nil {
			keys = append(keys, n)
		}
	}
	slices.Sort(keys)
	out := make([]catalog.Track, 0, len(keys))
	for _, k := range keys {
		var e entry
		if err := json.Unmarshal(raw[strconv.Itoa(k)], &e); err != nil || e.Song.V.ID == "" {
			continue
		}
		if e.Song.V.ID == fstr(id) {
			continue
		}
		out = append(out, c.track(e.Song.V))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (c *Client) createStation(ctx context.Context, songID string) (string, error) {
	ids, _ := json.Marshal([]string{url.QueryEscape(songID)})
	var r struct {
		StationID fstr `json:"stationid"`
	}
	params := url.Values{"entity_id": {string(ids)}, "entity_type": {"queue"}}
	if err := c.get(ctx, callCreateStation, params, ctxAndroid, &r); err != nil {
		return "", err
	}
	if r.StationID == "" {
		return "", catalog.NewError(catalog.KindUpstream, "could not start a related-songs station", nil)
	}
	return string(r.StationID), nil
}

func (c *Client) Album(ctx context.Context, id string) (*catalog.Album, error) {
	var r rawAlbum
	if err := c.get(ctx, callAlbum, url.Values{"albumid": {id}}, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.ID == "" {
		return nil, catalog.NotFound("album not found")
	}
	a := c.album(r)
	return &a, nil
}

func (c *Client) AlbumByLink(ctx context.Context, token string) (*catalog.Album, error) {
	var r rawAlbum
	if err := c.get(ctx, callWebAPI, url.Values{"token": {token}, "type": {"album"}}, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.ID == "" {
		return nil, catalog.NotFound("album not found")
	}
	a := c.album(r)
	return &a, nil
}

func artistParams(o catalog.ArtistOptions) url.Values {
	sortBy := o.SortBy
	if sortBy == "" {
		sortBy = catalog.SortPopularity
	}
	order := o.SortOrder
	if order == "" {
		order = "asc"
	}
	return url.Values{
		"n_song":     {strconv.Itoa(o.SongCount)},
		"n_album":    {strconv.Itoa(o.AlbumCount)},
		"page":       {strconv.Itoa(o.Page)},
		"sort_order": {order},
		"category":   {string(sortBy)},
	}
}

func (c *Client) Artist(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Artist, error) {
	p := artistParams(o)
	p.Set("artistId", id)
	var r rawArtist
	if err := c.get(ctx, callArtist, p, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.ArtistID == "" && r.ID == "" {
		return nil, catalog.NotFound("artist not found")
	}
	a := c.artist(r)
	return &a, nil
}

func (c *Client) ArtistByLink(ctx context.Context, token string, o catalog.ArtistOptions) (*catalog.Artist, error) {
	p := artistParams(o)
	p.Set("token", token)
	p.Set("type", "artist")
	var r rawArtist
	if err := c.get(ctx, callWebAPI, p, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.ArtistID == "" && r.ID == "" {
		return nil, catalog.NotFound("artist not found")
	}
	a := c.artist(r)
	return &a, nil
}

func (c *Client) ArtistSongs(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Page[catalog.Track], error) {
	p := artistParams(o)
	p.Del("n_song")
	p.Del("n_album")
	p.Set("artistId", id)
	var r rawArtistSongs
	if err := c.get(ctx, callArtistSongs, p, ctxWeb, &r); err != nil {
		return nil, err
	}
	songs := c.tracks(r.TopSongs.V.Songs)
	return &catalog.Page[catalog.Track]{Total: r.TopSongs.V.Total.Int(), Page: o.Page, Limit: len(songs), Results: songs}, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Page[catalog.Album], error) {
	p := artistParams(o)
	p.Del("n_song")
	p.Del("n_album")
	p.Set("artistId", id)
	var r rawArtistAlbums
	if err := c.get(ctx, callArtistAlbums, p, ctxWeb, &r); err != nil {
		return nil, err
	}
	albums := make([]catalog.Album, 0, len(r.TopAlbums.V.Albums))
	for _, a := range r.TopAlbums.V.Albums {
		if a.ID != "" {
			albums = append(albums, c.album(a))
		}
	}
	return &catalog.Page[catalog.Album]{Total: r.TopAlbums.V.Total.Int(), Page: o.Page, Limit: len(albums), Results: albums}, nil
}

func (c *Client) Playlist(ctx context.Context, id string, page, limit int) (*catalog.Playlist, error) {
	var r rawPlaylist
	p := url.Values{"listid": {id}, "n": {strconv.Itoa(limit)}, "p": {strconv.Itoa(max(page, 1))}}
	if err := c.get(ctx, callPlaylist, p, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.ID == "" {
		return nil, catalog.NotFound("playlist not found")
	}
	pl := c.playlist(r)
	if len(pl.Tracks) > limit {
		pl.Tracks = pl.Tracks[:limit]
	}
	return &pl, nil
}

func (c *Client) PlaylistByLink(ctx context.Context, token string, page, limit int) (*catalog.Playlist, error) {
	var r rawPlaylist
	p := url.Values{"token": {token}, "type": {"playlist"}, "n": {strconv.Itoa(limit)}, "p": {strconv.Itoa(max(page, 1))}}
	if err := c.get(ctx, callWebAPI, p, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.ID == "" {
		return nil, catalog.NotFound("playlist not found")
	}
	pl := c.playlist(r)
	if len(pl.Tracks) > limit {
		pl.Tracks = pl.Tracks[:limit]
	}
	return &pl, nil
}

func (c *Client) Lyrics(ctx context.Context, id string) (*catalog.Lyrics, error) {
	var r rawLyrics
	if err := c.get(ctx, callLyrics, url.Values{"lyrics_id": {id}}, ctxWeb, &r); err != nil {
		return nil, err
	}
	if r.Lyrics == "" {
		return nil, catalog.NotFound("lyrics are not available for this track")
	}
	return &catalog.Lyrics{
		TrackID:   catalog.MakeID(catalog.ProviderSaavn, id),
		Text:      strings.ReplaceAll(text(r.Lyrics), "<br>", "\n"),
		Snippet:   text(r.Snippet),
		Copyright: text(r.Copyright),
	}, nil
}

// Trending and Browse call endpoints listed in the reference project's
// constants but never used or documented there. Their response shapes are
// unverified, so they are parsed tolerantly and may return no collections.
func (c *Client) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	var raw json.RawMessage
	p := url.Values{"entity_type": {"song"}, "entity_language": {language}}
	if err := c.get(ctx, callTrending, p, ctxWeb, &raw); err != nil {
		return nil, err
	}
	return collections(raw, "trending"), nil
}

func (c *Client) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	var raw json.RawMessage
	if err := c.get(ctx, callBrowseModules, url.Values{"language": {language}}, ctxWeb, &raw); err != nil {
		return nil, err
	}
	return collections(raw, "modules"), nil
}
