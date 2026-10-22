package itunes

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
	ProviderName = "it"
	DefaultBase  = "https://itunes.apple.com"
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
	req.Header.Set("User-Agent", "Swar/1.0 (Apple iTunes Catalog Resolver)")

	resp, err := c.http.Do(req)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "itunes search api request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return catalog.NewError(catalog.KindRateLimited, "itunes rate limited", nil)
	}
	if resp.StatusCode == http.StatusNotFound {
		return catalog.NotFound("itunes entity not found")
	}
	if resp.StatusCode >= 400 {
		return catalog.NewError(catalog.KindUpstream, fmt.Sprintf("itunes returned status %d", resp.StatusCode), nil)
	}

	lr := io.LimitReader(resp.Body, c.maxBody)
	body, err := io.ReadAll(lr)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to read itunes response", err)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to decode itunes response", err)
	}
	return nil
}

type itunesItem struct {
	WrapperType       string `json:"wrapperType"`
	Kind              string `json:"kind"`
	ArtistID          int64  `json:"artistId"`
	CollectionID      int64  `json:"collectionId"`
	TrackID           int64  `json:"trackId"`
	ArtistName        string `json:"artistName"`
	CollectionName    string `json:"collectionName"`
	TrackName         string `json:"trackName"`
	TrackCensored     string `json:"trackCensoredName"`
	ArtistViewURL     string `json:"artistViewUrl"`
	CollectionViewURL string `json:"collectionViewUrl"`
	TrackViewURL      string `json:"trackViewUrl"`
	PreviewURL        string `json:"previewUrl"`
	ArtworkUrl30      string `json:"artworkUrl30"`
	ArtworkUrl60      string `json:"artworkUrl60"`
	ArtworkUrl100     string `json:"artworkUrl100"`
	ReleaseDate       string `json:"releaseDate"`
	TrackTimeMillis   int    `json:"trackTimeMillis"`
	PrimaryGenreName  string `json:"primaryGenreName"`
	TrackCount        int    `json:"trackCount"`
	Country           string `json:"country"`
	Explicitness      string `json:"trackExplicitness"`
}

type itunesResponse struct {
	ResultCount int          `json:"resultCount"`
	Results     []itunesItem `json:"results"`
}

func (c *Client) toTrack(item itunesItem) catalog.Track {
	idStr := strconv.FormatInt(item.TrackID, 10)
	title := normalize.CleanTitle(item.TrackName)

	var artists []catalog.ArtistRef
	if item.ArtistName != "" {
		artID := ""
		if item.ArtistID > 0 {
			artID = catalog.MakeID(ProviderName, strconv.FormatInt(item.ArtistID, 10))
		}
		artists = append(artists, catalog.ArtistRef{
			ID:   artID,
			Name: item.ArtistName,
			Role: "primary",
			URL:  item.ArtistViewURL,
		})
	}

	var album *catalog.AlbumRef
	if item.CollectionName != "" {
		albID := ""
		if item.CollectionID > 0 {
			albID = catalog.MakeID(ProviderName, strconv.FormatInt(item.CollectionID, 10))
		}
		album = &catalog.AlbumRef{
			ID:   albID,
			Name: item.CollectionName,
			URL:  item.CollectionViewURL,
		}
	}

	images := makeArtworkImages(item.ArtworkUrl100)

	meta := map[string]string{
		"itunes_id": idStr,
	}
	if item.PreviewURL != "" {
		meta["preview_url"] = item.PreviewURL
	}
	if item.Country != "" {
		meta["country"] = item.Country
	}

	year := 0
	if len(item.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(item.ReleaseDate[:4])
	}

	durationSec := item.TrackTimeMillis / 1000
	explicit := strings.EqualFold(item.Explicitness, "explicit")

	return catalog.Track{
		ID:           catalog.MakeID(ProviderName, idStr),
		Provider:     ProviderName,
		ProviderID:   idStr,
		Title:        title,
		Artists:      artists,
		Album:        album,
		DurationSec:  durationSec,
		Images:       images,
		Genre:        item.PrimaryGenreName,
		Year:         year,
		ReleaseDate:  item.ReleaseDate,
		Explicit:     explicit,
		URL:          item.TrackViewURL,
		Playable:     item.PreviewURL != "",
		Download:     catalog.DownloadEligibility{Eligible: false, Reason: "preview_only"},
		ProviderMeta: meta,
	}
}

func (c *Client) toAlbum(item itunesItem, tracks []catalog.Track) catalog.Album {
	idStr := strconv.FormatInt(item.CollectionID, 10)
	images := makeArtworkImages(item.ArtworkUrl100)

	var artists []catalog.ArtistRef
	if item.ArtistName != "" {
		artID := ""
		if item.ArtistID > 0 {
			artID = catalog.MakeID(ProviderName, strconv.FormatInt(item.ArtistID, 10))
		}
		artists = append(artists, catalog.ArtistRef{
			ID:   artID,
			Name: item.ArtistName,
			URL:  item.ArtistViewURL,
		})
	}

	year := 0
	if len(item.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(item.ReleaseDate[:4])
	}

	return catalog.Album{
		ID:         catalog.MakeID(ProviderName, idStr),
		Provider:   ProviderName,
		ProviderID: idStr,
		Name:       item.CollectionName,
		Artists:    artists,
		Images:     images,
		SongCount:  item.TrackCount,
		Year:       year,
		URL:        item.CollectionViewURL,
		Tracks:     tracks,
	}
}

func (c *Client) toArtist(item itunesItem) catalog.ArtistRef {
	idStr := strconv.FormatInt(item.ArtistID, 10)
	return catalog.ArtistRef{
		ID:   catalog.MakeID(ProviderName, idStr),
		Name: item.ArtistName,
		URL:  item.ArtistViewURL,
	}
}

// makeArtworkImages creates a multi-resolution image set from Apple's artwork URL
// by dynamically replacing the dimension token (100x100bb.jpg -> 300x300, 600x600, 1000x1000).
func makeArtworkImages(art100 string) []catalog.Image {
	if art100 == "" {
		return nil
	}
	var imgs []catalog.Image
	imgs = append(imgs, catalog.Image{Quality: "100x100", URL: art100})

	if strings.Contains(art100, "100x100") {
		imgs = append(imgs, catalog.Image{
			Quality: "300x300",
			URL:     strings.Replace(art100, "100x100", "300x300", 1),
		})
		imgs = append(imgs, catalog.Image{
			Quality: "600x600",
			URL:     strings.Replace(art100, "100x100", "600x600", 1),
		})
		imgs = append(imgs, catalog.Image{
			Quality: "1000x1000",
			URL:     strings.Replace(art100, "100x100", "1000x1000", 1),
		})
	}
	return imgs
}

func (c *Client) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"term":   {query},
		"media":  {"music"},
		"entity": {"song"},
		"limit":  {strconv.Itoa(limit)},
	}
	var resp itunesResponse
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Track, 0, len(resp.Results))
	for _, item := range resp.Results {
		if item.TrackID > 0 {
			out = append(out, c.toTrack(item))
		}
	}
	return out, nil
}

func (c *Client) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"term":   {query},
		"media":  {"music"},
		"entity": {"album"},
		"limit":  {strconv.Itoa(limit)},
	}
	var resp itunesResponse
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Album, 0, len(resp.Results))
	for _, item := range resp.Results {
		if item.CollectionID > 0 {
			out = append(out, c.toAlbum(item, nil))
		}
	}
	return out, nil
}

func (c *Client) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"term":   {query},
		"media":  {"music"},
		"entity": {"musicArtist"},
		"limit":  {strconv.Itoa(limit)},
	}
	var resp itunesResponse
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.ArtistRef, 0, len(resp.Results))
	for _, item := range resp.Results {
		if item.ArtistID > 0 {
			out = append(out, c.toArtist(item))
		}
	}
	return out, nil
}

func (c *Client) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error) {
	// iTunes Search API does not expose playlist search endpoints directly
	return []catalog.Playlist{}, nil
}

func (c *Client) Track(ctx context.Context, id string) (*catalog.Track, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	q := url.Values{"id": {id}, "entity": {"song"}}
	var resp itunesResponse
	if err := c.get(ctx, "/lookup", q, &resp); err != nil {
		return nil, err
	}
	for _, item := range resp.Results {
		if item.TrackID > 0 {
			t := c.toTrack(item)
			return &t, nil
		}
	}
	return nil, catalog.NotFound("itunes track not found")
}

func (c *Client) Album(ctx context.Context, id string) (*catalog.Album, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	q := url.Values{"id": {id}, "entity": {"song"}}
	var resp itunesResponse
	if err := c.get(ctx, "/lookup", q, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, catalog.NotFound("itunes album not found")
	}

	var albItem itunesItem
	var tracks []catalog.Track
	for _, item := range resp.Results {
		if item.WrapperType == "collection" {
			albItem = item
		} else if item.WrapperType == "track" && item.TrackID > 0 {
			tracks = append(tracks, c.toTrack(item))
		}
	}
	if albItem.CollectionID == 0 && len(resp.Results) > 0 {
		albItem = resp.Results[0]
	}
	alb := c.toAlbum(albItem, tracks)
	return &alb, nil
}

func (c *Client) Artist(ctx context.Context, id string) (*catalog.Artist, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	q := url.Values{"id": {id}, "entity": {"song"}, "limit": {"10"}}
	var resp itunesResponse
	if err := c.get(ctx, "/lookup", q, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, catalog.NotFound("itunes artist not found")
	}

	var artItem itunesItem
	var topSongs []catalog.Track
	for _, item := range resp.Results {
		if item.WrapperType == "artist" {
			artItem = item
		} else if item.WrapperType == "track" && item.TrackID > 0 {
			topSongs = append(topSongs, c.toTrack(item))
		}
	}
	if artItem.ArtistID == 0 && len(resp.Results) > 0 {
		artItem = resp.Results[0]
	}

	idStr := strconv.FormatInt(artItem.ArtistID, 10)
	albs, _ := c.ArtistAlbums(ctx, id, 10)

	return &catalog.Artist{
		ID:         catalog.MakeID(ProviderName, idStr),
		Provider:   ProviderName,
		ProviderID: idStr,
		Name:       artItem.ArtistName,
		URL:        artItem.ArtistViewURL,
		TopSongs:   topSongs,
		TopAlbums:  albs,
	}, nil
}

func (c *Client) ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	if limit <= 0 {
		limit = 10
	}
	q := url.Values{"id": {id}, "entity": {"song"}, "limit": {strconv.Itoa(limit)}}
	var resp itunesResponse
	if err := c.get(ctx, "/lookup", q, &resp); err != nil {
		return nil, err
	}
	var out []catalog.Track
	for _, item := range resp.Results {
		if item.WrapperType == "track" && item.TrackID > 0 {
			out = append(out, c.toTrack(item))
		}
	}
	return out, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	if limit <= 0 {
		limit = 10
	}
	q := url.Values{"id": {id}, "entity": {"album"}, "limit": {strconv.Itoa(limit)}}
	var resp itunesResponse
	if err := c.get(ctx, "/lookup", q, &resp); err != nil {
		return nil, err
	}
	var out []catalog.Album
	for _, item := range resp.Results {
		if item.WrapperType == "collection" && item.CollectionID > 0 {
			out = append(out, c.toAlbum(item, nil))
		}
	}
	return out, nil
}

func (c *Client) Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error) {
	return nil, catalog.Unavailable("playlist lookup not supported by itunes search api")
}

func (c *Client) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}

func (c *Client) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}

// MediaSources returns Apple's authorized 30-second 256kbps AAC audio preview stream.
func (c *Client) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	t, err := c.Track(ctx, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	preview := t.ProviderMeta["preview_url"]
	if preview == "" {
		return nil, time.Time{}, catalog.Unavailable("no audio preview available for this track")
	}

	expiry := time.Now().Add(24 * time.Hour)
	sources := []catalog.MediaSource{
		{
			Kind:     "audio",
			Quality:  "256kbps",
			URL:      preview,
			MimeType: "audio/m4a",
			Bitrate:  256,
		},
	}
	return sources, expiry, nil
}
