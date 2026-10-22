package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/normalize"
	"swar/internal/music/providers"
)

const (
	ProviderName = "dz"
	DefaultBase  = "https://api.deezer.com"
)

type Client struct {
	base    string
	http    *http.Client
	sem     chan struct{}
	timeout time.Duration
	maxBody int64
}

func New(opts providers.ClientOptions) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Timeout: 10 * time.Second,
		}
	}
	concurrency := opts.MaxConcurrency
	if concurrency < 1 {
		concurrency = 10
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 2 << 20 // 2MB
	}
	return &Client{
		base:    DefaultBase,
		http:    hc,
		sem:     make(chan struct{}, concurrency),
		timeout: timeout,
		maxBody: maxBody,
	}
}

func (c *Client) SetBaseURL(u string) {
	c.base = strings.TrimRight(u, "/")
}

func (c *Client) Name() string {
	return ProviderName
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := c.base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Swar/1.0 (Music Metadata Engine)")

	resp, err := c.http.Do(req)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "deezer api request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return catalog.NewError(catalog.KindRateLimited, "deezer rate limited", nil)
	}
	if resp.StatusCode == http.StatusNotFound {
		return catalog.NotFound("deezer entity not found")
	}
	if resp.StatusCode >= 400 {
		return catalog.NewError(catalog.KindUpstream, fmt.Sprintf("deezer returned status %d", resp.StatusCode), nil)
	}

	lr := io.LimitReader(resp.Body, c.maxBody)
	body, err := io.ReadAll(lr)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to read deezer response", err)
	}

	// Check for Deezer API level errors: {"error": {"type": "...", "message": "...", "code": ...}}
	var errCheck struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errCheck) == nil && errCheck.Error != nil {
		if errCheck.Error.Code == 4 || errCheck.Error.Code == 800 {
			return catalog.NewError(catalog.KindNotFound, errCheck.Error.Message, nil)
		}
		return catalog.NewError(catalog.KindUpstream, errCheck.Error.Message, nil)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to decode deezer response", err)
	}
	return nil
}

// Data structures for Deezer API responses
type deezerTrack struct {
	ID             int64        `json:"id"`
	Title          string       `json:"title"`
	TitleShort     string       `json:"title_short"`
	Link           string       `json:"link"`
	Duration       int          `json:"duration"`
	Rank           int          `json:"rank"`
	ExplicitLyrics bool         `json:"explicit_lyrics"`
	Preview        string       `json:"preview"`
	ISRC           string       `json:"isrc"`
	Artist         deezerArtist `json:"artist"`
	Album          deezerAlbum  `json:"album"`
	ReleaseDate    string       `json:"release_date"`
}

type deezerArtist struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Link          string `json:"link"`
	PictureSmall  string `json:"picture_small"`
	PictureMedium string `json:"picture_medium"`
	PictureBig    string `json:"picture_big"`
	PictureXL     string `json:"picture_xl"`
	NbFan         int    `json:"nb_fan"`
	NbAlbum       int    `json:"nb_album"`
}

type deezerAlbum struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	CoverSmall  string `json:"cover_small"`
	CoverMedium string `json:"cover_medium"`
	CoverBig    string `json:"cover_big"`
	CoverXL     string `json:"cover_xl"`
	GenreID     int    `json:"genre_id"`
	NbTracks    int    `json:"nb_tracks"`
	ReleaseDate string `json:"release_date"`
}

type deezerPlaylist struct {
	ID            int64        `json:"id"`
	Title         string       `json:"title"`
	Description   string       `json:"description"`
	Link          string       `json:"link"`
	PictureSmall  string       `json:"picture_small"`
	PictureMedium string       `json:"picture_medium"`
	PictureBig    string       `json:"picture_big"`
	PictureXL     string       `json:"picture_xl"`
	NbTracks      int          `json:"nb_tracks"`
	Fans          int64        `json:"fans"`
	Creator       deezerArtist `json:"creator"`
}

type deezerListResponse[T any] struct {
	Data  []T    `json:"data"`
	Total int    `json:"total"`
	Next  string `json:"next"`
}

type deezerAlbumDetail struct {
	deezerAlbum
	Artist deezerArtist `json:"artist"`
	Tracks struct {
		Data []deezerTrack `json:"data"`
	} `json:"tracks"`
}

type deezerPlaylistDetail struct {
	deezerPlaylist
	Tracks struct {
		Data []deezerTrack `json:"data"`
	} `json:"tracks"`
}

type deezerChartResponse struct {
	Tracks struct {
		Data []deezerTrack `json:"data"`
	} `json:"tracks"`
	Albums struct {
		Data []deezerAlbum `json:"data"`
	} `json:"albums"`
	Artists struct {
		Data []deezerArtist `json:"data"`
	} `json:"artists"`
	Playlists struct {
		Data []deezerPlaylist `json:"data"`
	} `json:"playlists"`
}

func (c *Client) toTrack(t deezerTrack) catalog.Track {
	idStr := strconv.FormatInt(t.ID, 10)
	title := t.Title
	if title == "" {
		title = t.TitleShort
	}
	cleanTitle := normalize.CleanTitle(title)

	var artists []catalog.ArtistRef
	if t.Artist.ID > 0 || t.Artist.Name != "" {
		artID := strconv.FormatInt(t.Artist.ID, 10)
		artists = append(artists, catalog.ArtistRef{
			ID:    catalog.MakeID(ProviderName, artID),
			Name:  t.Artist.Name,
			Role:  "primary",
			URL:   t.Artist.Link,
			Image: makeImages(t.Artist.PictureSmall, t.Artist.PictureMedium, t.Artist.PictureBig, t.Artist.PictureXL),
		})
	}

	var album *catalog.AlbumRef
	if t.Album.ID > 0 || t.Album.Title != "" {
		albID := strconv.FormatInt(t.Album.ID, 10)
		album = &catalog.AlbumRef{
			ID:   catalog.MakeID(ProviderName, albID),
			Name: t.Album.Title,
			URL:  t.Album.Link,
		}
	}

	images := makeImages(t.Album.CoverSmall, t.Album.CoverMedium, t.Album.CoverBig, t.Album.CoverXL)
	if len(images) == 0 && len(artists) > 0 {
		images = artists[0].Image
	}

	meta := map[string]string{
		"deezer_id": idStr,
	}
	if t.Preview != "" {
		meta["preview_url"] = t.Preview
	}
	if t.ISRC != "" {
		meta["isrc"] = t.ISRC
	}

	year := 0
	if len(t.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(t.ReleaseDate[:4])
	}

	return catalog.Track{
		ID:           catalog.MakeID(ProviderName, idStr),
		Provider:     ProviderName,
		ProviderID:   idStr,
		Title:        cleanTitle,
		Artists:      artists,
		Album:        album,
		DurationSec:  t.Duration,
		Images:       images,
		Explicit:     t.ExplicitLyrics,
		PlayCount:    int64(t.Rank),
		URL:          t.Link,
		Playable:     t.Preview != "",
		Year:         year,
		ReleaseDate:  t.ReleaseDate,
		Download:     catalog.DownloadEligibility{Eligible: false, Reason: "preview_only"},
		ProviderMeta: meta,
	}
}

func (c *Client) toAlbum(a deezerAlbum, artistName string, tracks []catalog.Track) catalog.Album {
	idStr := strconv.FormatInt(a.ID, 10)
	images := makeImages(a.CoverSmall, a.CoverMedium, a.CoverBig, a.CoverXL)

	var artists []catalog.ArtistRef
	if artistName != "" {
		artists = append(artists, catalog.ArtistRef{Name: artistName})
	}

	year := 0
	if len(a.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(a.ReleaseDate[:4])
	}

	return catalog.Album{
		ID:         catalog.MakeID(ProviderName, idStr),
		Provider:   ProviderName,
		ProviderID: idStr,
		Name:       a.Title,
		URL:        a.Link,
		SongCount:  a.NbTracks,
		Artists:    artists,
		Images:     images,
		Tracks:     tracks,
		Year:       year,
	}
}

func (c *Client) toArtist(a deezerArtist) catalog.ArtistRef {
	idStr := strconv.FormatInt(a.ID, 10)
	count := a.NbAlbum
	return catalog.ArtistRef{
		ID:        catalog.MakeID(ProviderName, idStr),
		Name:      a.Name,
		URL:       a.Link,
		Image:     makeImages(a.PictureSmall, a.PictureMedium, a.PictureBig, a.PictureXL),
		SongCount: &count,
	}
}

func (c *Client) toPlaylist(p deezerPlaylist, tracks []catalog.Track) catalog.Playlist {
	idStr := strconv.FormatInt(p.ID, 10)
	images := makeImages(p.PictureSmall, p.PictureMedium, p.PictureBig, p.PictureXL)
	var artists []catalog.ArtistRef
	if p.Creator.Name != "" {
		artists = append(artists, catalog.ArtistRef{Name: p.Creator.Name})
	}

	return catalog.Playlist{
		ID:          catalog.MakeID(ProviderName, idStr),
		Provider:    ProviderName,
		ProviderID:  idStr,
		Name:        p.Title,
		Description: p.Description,
		URL:         p.Link,
		SongCount:   p.NbTracks,
		PlayCount:   p.Fans,
		Artists:     artists,
		Images:      images,
		Tracks:      tracks,
	}
}

func makeImages(small, medium, big, xl string) []catalog.Image {
	var imgs []catalog.Image
	if small != "" {
		imgs = append(imgs, catalog.Image{Quality: "50x50", URL: small})
	}
	if medium != "" {
		imgs = append(imgs, catalog.Image{Quality: "120x120", URL: medium})
	}
	if big != "" {
		imgs = append(imgs, catalog.Image{Quality: "500x500", URL: big})
	}
	if xl != "" {
		imgs = append(imgs, catalog.Image{Quality: "1000x1000", URL: xl})
	}
	return imgs
}

func (c *Client) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error) {
	if limit <= 0 {
		limit = 25
	}
	var resp deezerListResponse[deezerTrack]
	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Track, 0, len(resp.Data))
	for _, t := range resp.Data {
		out = append(out, c.toTrack(t))
	}
	return out, nil
}

func (c *Client) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error) {
	if limit <= 0 {
		limit = 25
	}
	var resp deezerListResponse[deezerAlbum]
	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/search/album", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Album, 0, len(resp.Data))
	for _, a := range resp.Data {
		out = append(out, c.toAlbum(a, "", nil))
	}
	return out, nil
}

func (c *Client) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error) {
	if limit <= 0 {
		limit = 25
	}
	var resp deezerListResponse[deezerArtist]
	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/search/artist", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.ArtistRef, 0, len(resp.Data))
	for _, a := range resp.Data {
		out = append(out, c.toArtist(a))
	}
	return out, nil
}

func (c *Client) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error) {
	if limit <= 0 {
		limit = 25
	}
	var resp deezerListResponse[deezerPlaylist]
	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/search/playlist", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Playlist, 0, len(resp.Data))
	for _, p := range resp.Data {
		out = append(out, c.toPlaylist(p, nil))
	}
	return out, nil
}

func (c *Client) Track(ctx context.Context, id string) (*catalog.Track, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	var t deezerTrack
	if err := c.get(ctx, "/track/"+url.PathEscape(id), nil, &t); err != nil {
		return nil, err
	}
	track := c.toTrack(t)
	return &track, nil
}

func (c *Client) Album(ctx context.Context, id string) (*catalog.Album, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	var a deezerAlbumDetail
	if err := c.get(ctx, "/album/"+url.PathEscape(id), nil, &a); err != nil {
		return nil, err
	}
	tracks := make([]catalog.Track, 0, len(a.Tracks.Data))
	for _, t := range a.Tracks.Data {
		if t.Album.ID == 0 {
			t.Album = a.deezerAlbum
		}
		tracks = append(tracks, c.toTrack(t))
	}
	alb := c.toAlbum(a.deezerAlbum, a.Artist.Name, tracks)
	return &alb, nil
}

func (c *Client) Artist(ctx context.Context, id string) (*catalog.Artist, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	var a deezerArtist
	if err := c.get(ctx, "/artist/"+url.PathEscape(id), nil, &a); err != nil {
		return nil, err
	}

	top, _ := c.ArtistTopTracks(ctx, id, 10)
	albs, _ := c.ArtistAlbums(ctx, id, 10)

	idStr := strconv.FormatInt(a.ID, 10)
	return &catalog.Artist{
		ID:            catalog.MakeID(ProviderName, idStr),
		Provider:      ProviderName,
		ProviderID:    idStr,
		Name:          a.Name,
		URL:           a.Link,
		Images:        makeImages(a.PictureSmall, a.PictureMedium, a.PictureBig, a.PictureXL),
		FollowerCount: int64(a.NbFan),
		TopSongs:      top,
		TopAlbums:     albs,
	}, nil
}

func (c *Client) ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	if limit <= 0 {
		limit = 10
	}
	var resp deezerListResponse[deezerTrack]
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/artist/"+url.PathEscape(id)+"/top", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Track, 0, len(resp.Data))
	for _, t := range resp.Data {
		out = append(out, c.toTrack(t))
	}
	return out, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	if limit <= 0 {
		limit = 10
	}
	var resp deezerListResponse[deezerAlbum]
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/artist/"+url.PathEscape(id)+"/albums", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Album, 0, len(resp.Data))
	for _, a := range resp.Data {
		out = append(out, c.toAlbum(a, "", nil))
	}
	return out, nil
}

func (c *Client) Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	var p deezerPlaylistDetail
	if err := c.get(ctx, "/playlist/"+url.PathEscape(id), nil, &p); err != nil {
		return nil, err
	}
	tracks := make([]catalog.Track, 0, len(p.Tracks.Data))
	for _, t := range p.Tracks.Data {
		tracks = append(tracks, c.toTrack(t))
	}
	pl := c.toPlaylist(p.deezerPlaylist, tracks)
	return &pl, nil
}

func (c *Client) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	var ch deezerChartResponse
	if err := c.get(ctx, "/chart", nil, &ch); err != nil {
		return nil, err
	}

	var collections []catalog.Collection

	// Trending Songs Collection
	if len(ch.Tracks.Data) > 0 {
		items := make([]catalog.Item, 0, len(ch.Tracks.Data))
		for _, t := range ch.Tracks.Data {
			items = append(items, catalog.Item{
				Type:     "song",
				ID:       catalog.MakeID(ProviderName, strconv.FormatInt(t.ID, 10)),
				Title:    t.Title,
				Subtitle: t.Artist.Name,
				URL:      t.Link,
				Images:   makeImages(t.Album.CoverSmall, t.Album.CoverMedium, t.Album.CoverBig, t.Album.CoverXL),
			})
		}
		collections = append(collections, catalog.Collection{
			Key:   "trending_songs",
			Title: "Trending Songs",
			Items: items,
		})
	}

	// Top Albums Collection
	if len(ch.Albums.Data) > 0 {
		items := make([]catalog.Item, 0, len(ch.Albums.Data))
		for _, a := range ch.Albums.Data {
			items = append(items, catalog.Item{
				Type:   "album",
				ID:     catalog.MakeID(ProviderName, strconv.FormatInt(a.ID, 10)),
				Title:  a.Title,
				URL:    a.Link,
				Images: makeImages(a.CoverSmall, a.CoverMedium, a.CoverBig, a.CoverXL),
			})
		}
		collections = append(collections, catalog.Collection{
			Key:   "top_albums",
			Title: "Top Albums",
			Items: items,
		})
	}

	// Featured Playlists Collection
	if len(ch.Playlists.Data) > 0 {
		items := make([]catalog.Item, 0, len(ch.Playlists.Data))
		for _, p := range ch.Playlists.Data {
			items = append(items, catalog.Item{
				Type:     "playlist",
				ID:       catalog.MakeID(ProviderName, strconv.FormatInt(p.ID, 10)),
				Title:    p.Title,
				Subtitle: p.Creator.Name,
				URL:      p.Link,
				Images:   makeImages(p.PictureSmall, p.PictureMedium, p.PictureBig, p.PictureXL),
			})
		}
		collections = append(collections, catalog.Collection{
			Key:   "top_playlists",
			Title: "Popular Playlists",
			Items: items,
		})
	}

	return collections, nil
}

func (c *Client) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	return c.Trending(ctx, language)
}

// MediaSources returns Deezer's authorized 30-second 128kbps MP3 preview stream.
func (c *Client) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	t, err := c.Track(ctx, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	preview := t.ProviderMeta["preview_url"]
	if preview == "" {
		return nil, time.Time{}, catalog.Unavailable("no audio preview available for this track")
	}

	// Previews remain valid on Deezer CDN for 24 hours.
	expiry := time.Now().Add(24 * time.Hour)
	sources := []catalog.MediaSource{
		{
			Kind:     "audio",
			Quality:  "128kbps",
			URL:      preview,
			MimeType: "audio/mp3",
			Bitrate:  128,
		},
	}
	return sources, expiry, nil
}
