package musicbrainz

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
	ProviderName = "mb"
	DefaultBase  = "https://musicbrainz.org/ws/2"
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
		concurrency = 2 // Respectful rate limiting for MusicBrainz
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 2 << 20
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

	if query == nil {
		query = url.Values{}
	}
	query.Set("fmt", "json")

	endpoint := c.base + path + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Swar/1.0 ( https://github.com/himanshusingh/Swar )")

	resp, err := c.http.Do(req)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "musicbrainz api request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
		return catalog.NewError(catalog.KindRateLimited, "musicbrainz rate limited", nil)
	}
	if resp.StatusCode == http.StatusNotFound {
		return catalog.NotFound("musicbrainz entity not found")
	}
	if resp.StatusCode >= 400 {
		return catalog.NewError(catalog.KindUpstream, fmt.Sprintf("musicbrainz returned status %d", resp.StatusCode), nil)
	}

	lr := io.LimitReader(resp.Body, c.maxBody)
	body, err := io.ReadAll(lr)
	if err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to read musicbrainz response", err)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return catalog.NewError(catalog.KindUpstream, "failed to decode musicbrainz response", err)
	}
	return nil
}

type mbArtistCredit struct {
	Name       string `json:"name"`
	Joinphrase string `json:"joinphrase"`
	Artist     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type mbRelease struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Date       string `json:"date"`
	Country    string `json:"country"`
	TrackCount int    `json:"track-count"`
}

type mbTag struct {
	Count int    `json:"count"`
	Name  string `json:"name"`
}

type mbRecording struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Length       int              `json:"length"` // in milliseconds
	Disambig     string           `json:"disambiguation"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
	Releases     []mbRelease      `json:"releases"`
	ISRCs        []string         `json:"isrcs"`
	Tags         []mbTag          `json:"tags"`
}

type mbRecordingSearchResponse struct {
	Count      int           `json:"count"`
	Recordings []mbRecording `json:"recordings"`
}

type mbReleaseSearchResponse struct {
	Count    int         `json:"count"`
	Releases []mbRelease `json:"releases"`
}

type mbArtist struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SortName string `json:"sort-name"`
	Disambig string `json:"disambiguation"`
	Country  string `json:"country"`
}

type mbArtistSearchResponse struct {
	Count   int        `json:"count"`
	Artists []mbArtist `json:"artists"`
}

func (c *Client) toTrack(r mbRecording) catalog.Track {
	title := normalize.CleanTitle(r.Title)

	var artists []catalog.ArtistRef
	for _, ac := range r.ArtistCredit {
		name := ac.Name
		if name == "" {
			name = ac.Artist.Name
		}
		if name != "" {
			artists = append(artists, catalog.ArtistRef{
				ID:   catalog.MakeID(ProviderName, ac.Artist.ID),
				Name: name,
				Role: "primary",
			})
		}
	}

	var album *catalog.AlbumRef
	year := 0
	releaseDate := ""
	if len(r.Releases) > 0 {
		rel := r.Releases[0]
		album = &catalog.AlbumRef{
			ID:   catalog.MakeID(ProviderName, rel.ID),
			Name: rel.Title,
		}
		releaseDate = rel.Date
		if len(rel.Date) >= 4 {
			year, _ = strconv.Atoi(rel.Date[:4])
		}
	}

	meta := map[string]string{
		"mbid": r.ID,
	}
	if len(r.ISRCs) > 0 && r.ISRCs[0] != "" {
		meta["isrc"] = r.ISRCs[0]
	}

	genre := ""
	if len(r.Tags) > 0 {
		genre = r.Tags[0].Name
	}

	durationSec := r.Length / 1000

	return catalog.Track{
		ID:           catalog.MakeID(ProviderName, r.ID),
		Provider:     ProviderName,
		ProviderID:   r.ID,
		Title:        title,
		Artists:      artists,
		Album:        album,
		DurationSec:  durationSec,
		Year:         year,
		ReleaseDate:  releaseDate,
		Genre:        genre,
		Playable:     false, // MusicBrainz is a pure metadata encyclopedia
		Download:     catalog.DownloadEligibility{Eligible: false, Reason: "metadata_only"},
		ProviderMeta: meta,
	}
}

func (c *Client) SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"query": {query},
		"limit": {strconv.Itoa(limit)},
	}
	var resp mbRecordingSearchResponse
	if err := c.get(ctx, "/recording/", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Track, 0, len(resp.Recordings))
	for _, rec := range resp.Recordings {
		out = append(out, c.toTrack(rec))
	}
	return out, nil
}

func (c *Client) SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"query": {query},
		"limit": {strconv.Itoa(limit)},
	}
	var resp mbReleaseSearchResponse
	if err := c.get(ctx, "/release/", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.Album, 0, len(resp.Releases))
	for _, rel := range resp.Releases {
		year := 0
		if len(rel.Date) >= 4 {
			year, _ = strconv.Atoi(rel.Date[:4])
		}
		out = append(out, catalog.Album{
			ID:         catalog.MakeID(ProviderName, rel.ID),
			Provider:   ProviderName,
			ProviderID: rel.ID,
			Name:       rel.Title,
			SongCount:  rel.TrackCount,
			Year:       year,
		})
	}
	return out, nil
}

func (c *Client) SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"query": {query},
		"limit": {strconv.Itoa(limit)},
	}
	var resp mbArtistSearchResponse
	if err := c.get(ctx, "/artist/", q, &resp); err != nil {
		return nil, err
	}
	out := make([]catalog.ArtistRef, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		out = append(out, catalog.ArtistRef{
			ID:   catalog.MakeID(ProviderName, a.ID),
			Name: a.Name,
		})
	}
	return out, nil
}

func (c *Client) SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error) {
	return []catalog.Playlist{}, nil
}

func (c *Client) Track(ctx context.Context, id string) (*catalog.Track, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	q := url.Values{
		"inc": {"artist-credits+releases+isrcs+genres"},
	}
	var rec mbRecording
	if err := c.get(ctx, "/recording/"+url.PathEscape(id), q, &rec); err != nil {
		return nil, err
	}
	t := c.toTrack(rec)
	return &t, nil
}

func (c *Client) Album(ctx context.Context, id string) (*catalog.Album, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	q := url.Values{
		"inc": {"recordings+artist-credits"},
	}
	var rel struct {
		mbRelease
		Media []struct {
			Tracks []struct {
				Recording mbRecording `json:"recording"`
			} `json:"tracks"`
		} `json:"media"`
	}
	if err := c.get(ctx, "/release/"+url.PathEscape(id), q, &rel); err != nil {
		return nil, err
	}
	var tracks []catalog.Track
	for _, m := range rel.Media {
		for _, tr := range m.Tracks {
			if tr.Recording.ID != "" {
				tracks = append(tracks, c.toTrack(tr.Recording))
			}
		}
	}
	year := 0
	if len(rel.Date) >= 4 {
		year, _ = strconv.Atoi(rel.Date[:4])
	}
	return &catalog.Album{
		ID:         catalog.MakeID(ProviderName, rel.ID),
		Provider:   ProviderName,
		ProviderID: rel.ID,
		Name:       rel.Title,
		SongCount:  rel.TrackCount,
		Year:       year,
		Tracks:     tracks,
	}, nil
}

func (c *Client) Artist(ctx context.Context, id string) (*catalog.Artist, error) {
	id = strings.TrimPrefix(id, ProviderName+":")
	var a mbArtist
	if err := c.get(ctx, "/artist/"+url.PathEscape(id), nil, &a); err != nil {
		return nil, err
	}
	return &catalog.Artist{
		ID:         catalog.MakeID(ProviderName, a.ID),
		Provider:   ProviderName,
		ProviderID: a.ID,
		Name:       a.Name,
	}, nil
}

func (c *Client) ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	return []catalog.Track{}, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error) {
	return []catalog.Album{}, nil
}

func (c *Client) Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error) {
	return nil, catalog.Unavailable("playlist lookup not supported by musicbrainz")
}

func (c *Client) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}

func (c *Client) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	return []catalog.Collection{}, nil
}

func (c *Client) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	return nil, time.Time{}, catalog.Unavailable("musicbrainz provides metadata only, no audio streams")
}
