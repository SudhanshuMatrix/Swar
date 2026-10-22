package spotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
	"swar/internal/music/normalize"
	"swar/internal/music/providers"
)

const (
	ProviderName = "sp"
	DefaultBase  = "https://api.spotify.com/v1"
	AuthEndpoint = "https://accounts.spotify.com/api/token"
)

type Options struct {
	providers.ClientOptions
	ClientID     string
	ClientSecret string
	AuthURL      string
}

type Client struct {
	base         string
	authURL      string
	clientID     string
	clientSecret string
	http         *http.Client
	sem          chan struct{}
	timeout      time.Duration
	maxBody      int64

	tokenMu   sync.RWMutex
	token     string
	expiresAt time.Time
}

func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Timeout: 10 * time.Second,
		}
	}
	concurrency := opts.MaxConcurrency
	if concurrency < 1 {
		concurrency = 8
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 2 << 20
	}
	authURL := opts.AuthURL
	if authURL == "" {
		authURL = AuthEndpoint
	}
	return &Client{
		base:         DefaultBase,
		authURL:      authURL,
		clientID:     opts.ClientID,
		clientSecret: opts.ClientSecret,
		http:         hc,
		sem:          make(chan struct{}, concurrency),
		timeout:      timeout,
		maxBody:      maxBody,
	}
}

func (c *Client) SetBaseURL(u string) {
	c.base = strings.TrimRight(u, "/")
}

func (c *Client) Name() string {
	return ProviderName
}

func (c *Client) Enabled() bool {
	return c.clientID != "" && c.clientSecret != ""
}

// ensureToken obtains or refreshes the Spotify OAuth2 Client Credentials access token.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	if !c.Enabled() {
		return "", catalog.Unavailable("spotify client credentials are not configured")
	}

	c.tokenMu.RLock()
	if c.token != "" && time.Now().Before(c.expiresAt) {
		tok := c.token
		c.tokenMu.RUnlock()
		return tok, nil
	}
	c.tokenMu.RUnlock()

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double check after acquiring write lock
	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, nil
	}

	data := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	creds := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", catalog.NewError(catalog.KindUpstream, "spotify auth request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", catalog.NewError(catalog.KindUpstream, fmt.Sprintf("spotify auth returned status %d", resp.StatusCode), nil)
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", catalog.NewError(catalog.KindUpstream, "failed to decode spotify auth response", err)
	}

	c.token = authResp.AccessToken
	// Margin of 60 seconds before expiry
	c.expiresAt = time.Now().Add(time.Duration(authResp.ExpiresIn-60) * time.Second)
	return c.token, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}

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
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "spotify api request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return catalog.NewError(catalog.KindRateLimited, "spotify rate limited", nil)
	}
	if resp.StatusCode == http.StatusNotFound {
		return catalog.NotFound("spotify entity not found")
	}
	if resp.StatusCode >= 400 {
		return catalog.NewError(catalog.KindUpstream, fmt.Sprintf("spotify returned status %d", resp.StatusCode), nil)
	}

	lr := io.LimitReader(resp.Body, c.maxBody)
	body, err := io.ReadAll(lr)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to read spotify response", err)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to decode spotify response", err)
	}
	return nil
}

type spImage struct {
	URL    string `json:"url"`
	Height int    `json:"height"`
	Width  int    `json:"width"`
}

type spArtist struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Images []spImage `json:"images"`
}

type spAlbum struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ReleaseDate string    `json:"release_date"`
	Images      []spImage `json:"images"`
	TotalTracks int       `json:"total_tracks"`
}

type spTrack struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DurationMs  int    `json:"duration_ms"`
	Explicit    bool   `json:"explicit"`
	Popularity  int    `json:"popularity"`
	PreviewURL  string `json:"preview_url"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	Artists []spArtist `json:"artists"`
	Album   spAlbum    `json:"album"`
}

type spSearchResponse struct {
	Tracks struct {
		Items []spTrack `json:"items"`
	} `json:"tracks"`
	Albums struct {
		Items []spAlbum `json:"items"`
	} `json:"albums"`
	Artists struct {
		Items []spArtist `json:"items"`
	} `json:"artists"`
}

func (c *Client) toTrack(t spTrack) catalog.Track {
	title := normalize.CleanTitle(t.Name)

	var artists []catalog.ArtistRef
	for _, a := range t.Artists {
		artists = append(artists, catalog.ArtistRef{
			ID:   catalog.MakeID(ProviderName, a.ID),
			Name: a.Name,
			Role: "primary",
		})
	}

	var album *catalog.AlbumRef
	if t.Album.ID != "" || t.Album.Name != "" {
		album = &catalog.AlbumRef{
			ID:   catalog.MakeID(ProviderName, t.Album.ID),
			Name: t.Album.Name,
		}
	}

	var images []catalog.Image
	for _, img := range t.Album.Images {
		quality := fmt.Sprintf("%dx%d", img.Width, img.Height)
		images = append(images, catalog.Image{Quality: quality, URL: img.URL})
	}

	meta := map[string]string{
		"spotify_id": t.ID,
	}
	if t.ExternalIDs.ISRC != "" {
		meta["isrc"] = t.ExternalIDs.ISRC
	}
	if t.PreviewURL != "" {
		meta["preview_url"] = t.PreviewURL
	}

	year := 0
	if len(t.Album.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(t.Album.ReleaseDate[:4])
	}

	durationSec := t.DurationMs / 1000

	return catalog.Track{
		ID:           catalog.MakeID(ProviderName, t.ID),
		Provider:     ProviderName,
		ProviderID:   t.ID,
		Title:        title,
		Artists:      artists,
		Album:        album,
		DurationSec:  durationSec,
		Images:       images,
		Explicit:     t.Explicit,
		PlayCount:    int64(t.Popularity),
		Playable:     t.PreviewURL != "",
		Year:         year,
		ReleaseDate:  t.Album.ReleaseDate,
		Download:     catalog.DownloadEligibility{Eligible: false, Reason: "preview_only"},
		ProviderMeta: meta,
	}
}

func (c *Client) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error) {
	if !c.Enabled() {
		return []catalog.Track{}, nil
	}
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"q":     {query},
		"type":  {"track"},
		"limit": {strconv.Itoa(limit)},
	}
	var resp spSearchResponse
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Track, 0, len(resp.Tracks.Items))
	for _, t := range resp.Tracks.Items {
		out = append(out, c.toTrack(t))
	}
	return out, nil
}

func (c *Client) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error) {
	if !c.Enabled() {
		return []catalog.Album{}, nil
	}
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"q":     {query},
		"type":  {"album"},
		"limit": {strconv.Itoa(limit)},
	}
	var resp spSearchResponse
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Album, 0, len(resp.Albums.Items))
	for _, a := range resp.Albums.Items {
		var imgs []catalog.Image
		for _, img := range a.Images {
			imgs = append(imgs, catalog.Image{Quality: fmt.Sprintf("%dx%d", img.Width, img.Height), URL: img.URL})
		}
		year := 0
		if len(a.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(a.ReleaseDate[:4])
		}
		out = append(out, catalog.Album{
			ID:         catalog.MakeID(ProviderName, a.ID),
			Provider:   ProviderName,
			ProviderID: a.ID,
			Name:       a.Name,
			Images:     imgs,
			SongCount:  a.TotalTracks,
			Year:       year,
		})
	}
	return out, nil
}

func (c *Client) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error) {
	if !c.Enabled() {
		return []catalog.ArtistRef{}, nil
	}
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"q":     {query},
		"type":  {"artist"},
		"limit": {strconv.Itoa(limit)},
	}
	var resp spSearchResponse
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.ArtistRef, 0, len(resp.Artists.Items))
	for _, a := range resp.Artists.Items {
		var imgs []catalog.Image
		for _, img := range a.Images {
			imgs = append(imgs, catalog.Image{Quality: fmt.Sprintf("%dx%d", img.Width, img.Height), URL: img.URL})
		}
		out = append(out, catalog.ArtistRef{
			ID:    catalog.MakeID(ProviderName, a.ID),
			Name:  a.Name,
			Image: imgs,
		})
	}
	return out, nil
}

func (c *Client) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error) {
	return []catalog.Playlist{}, nil
}

func (c *Client) Track(ctx context.Context, id string) (*catalog.Track, error) {
	if !c.Enabled() {
		return nil, catalog.Unavailable("spotify is not enabled")
	}
	id = strings.TrimPrefix(id, ProviderName+":")
	var t spTrack
	if err := c.get(ctx, "/tracks/"+url.PathEscape(id), nil, &t); err != nil {
		return nil, err
	}
	track := c.toTrack(t)
	return &track, nil
}

func (c *Client) Album(ctx context.Context, id string) (*catalog.Album, error) {
	return nil, catalog.Unavailable("spotify album details not implemented")
}

func (c *Client) Artist(ctx context.Context, id string) (*catalog.Artist, error) {
	return nil, catalog.Unavailable("spotify artist details not implemented")
}

func (c *Client) ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	return []catalog.Track{}, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error) {
	return []catalog.Album{}, nil
}

func (c *Client) Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error) {
	return nil, catalog.Unavailable("spotify playlist details not implemented")
}

func (c *Client) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}

func (c *Client) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}

func (c *Client) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	t, err := c.Track(ctx, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	preview := t.ProviderMeta["preview_url"]
	if preview == "" {
		return nil, time.Time{}, catalog.Unavailable("no preview stream available")
	}
	return []catalog.MediaSource{
		{
			Kind:     "audio",
			Quality:  "160kbps",
			URL:      preview,
			MimeType: "audio/mp3",
			Bitrate:  160,
		},
	}, time.Now().Add(time.Hour), nil
}
