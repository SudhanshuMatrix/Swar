// Package ytmusic searches YouTube Music for songs through its public web
// API (the same request music.youtube.com makes, no key or sign-in). It
// only finds songs; playback happens on the user's device from the video
// ID, so nothing here touches YouTube's media servers.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"swar/internal/catalog"
)

const (
	endpoint = "https://music.youtube.com/youtubei/v1/search?prettyPrint=false"
	// songsOnly is YouTube Music's "Songs" filter (official audio, not
	// music videos or uploads).
	songsOnly     = "EgWKAQIIAWoMEA4QChADEAQQCRAF"
	clientVersion = "1.20241023.01.00"
	maxBody       = 4 << 20
)

type Client struct {
	http     *http.Client
	endpoint string
}

func New(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	return &Client{http: &http.Client{Timeout: timeout}, endpoint: endpoint}
}

// SearchSongs returns up to limit songs for q.
func (c *Client) SearchSongs(ctx context.Context, q string, limit int) ([]catalog.Track, error) {
	body, _ := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName": "WEB_REMIX", "clientVersion": clientVersion, "hl": "en", "gl": "IN",
		}},
		"query":  q,
		"params": songsOnly,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music search: status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, err
	}
	return Parse(raw, limit)
}

// Parse pulls songs out of a search response. It looks for every
// musicResponsiveListItemRenderer wherever it sits, so small layout changes
// on YouTube's side don't break it.
func Parse(raw []byte, limit int) ([]catalog.Track, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var items []map[string]any
	collect(doc, &items)
	out := []catalog.Track{}
	seen := map[string]bool{}
	for _, it := range items {
		t, ok := track(it)
		if !ok || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func collect(v any, out *[]map[string]any) {
	switch n := v.(type) {
	case map[string]any:
		if item, ok := n["musicResponsiveListItemRenderer"].(map[string]any); ok {
			*out = append(*out, item)
			return
		}
		for _, child := range n {
			collect(child, out)
		}
	case []any:
		for _, child := range n {
			collect(child, out)
		}
	}
}

var durationRe = regexp.MustCompile(`^(\d+:)?\d{1,2}:\d{2}$`)

func track(it map[string]any) (catalog.Track, bool) {
	videoID := str(it, "playlistItemData", "videoId")
	cols, _ := it["flexColumns"].([]any)
	if len(cols) == 0 {
		return catalog.Track{}, false
	}
	titleRuns := runs(cols[0])
	if len(titleRuns) == 0 {
		return catalog.Track{}, false
	}
	title := str(titleRuns[0], "text")
	if videoID == "" {
		videoID = str(titleRuns[0], "navigationEndpoint", "watchEndpoint", "videoId")
	}
	if videoID == "" || title == "" {
		return catalog.Track{}, false
	}
	t := catalog.Track{
		ID: catalog.MakeID(catalog.ProviderYouTube, videoID), Provider: catalog.ProviderYouTube, ProviderID: videoID,
		Title: title, Playable: true,
		Download: catalog.DownloadEligibility{Eligible: false, Reason: "youtube"},
	}
	var details []map[string]any
	for _, col := range cols[1:] {
		details = append(details, runs(col)...)
	}
	for _, r := range details {
		text := strings.TrimSpace(str(r, "text"))
		page := str(r, "navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs",
			"browseEndpointContextMusicConfig", "pageType")
		id := str(r, "navigationEndpoint", "browseEndpoint", "browseId")
		switch {
		case text == "" || text == "•" || text == "&":
			continue
		case page == "MUSIC_PAGE_TYPE_ARTIST" || page == "MUSIC_PAGE_TYPE_USER_CHANNEL":
			t.Artists = append(t.Artists, catalog.ArtistRef{ID: id, Name: text, Role: "primary", Image: []catalog.Image{}})
		case page == "MUSIC_PAGE_TYPE_ALBUM":
			t.Album = &catalog.AlbumRef{ID: id, Name: text}
		case durationRe.MatchString(text):
			t.DurationSec = seconds(text)
		}
	}
	if len(t.Artists) == 0 {
		// Some songs name the artist without a link: take the first plain run.
		for _, r := range details {
			text := strings.TrimSpace(str(r, "text"))
			if text != "" && text != "•" && text != "Song" && !durationRe.MatchString(text) {
				t.Artists = []catalog.ArtistRef{{Name: text, Role: "primary", Image: []catalog.Image{}}}
				break
			}
		}
	}
	if thumbs, ok := dig(it, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails").([]any); ok && len(thumbs) > 0 {
		if u := str(thumbs[len(thumbs)-1], "url"); u != "" {
			t.Images = []catalog.Image{{Quality: "500x500", URL: bigger(u)}}
		}
	}
	return t, true
}

// bigger asks Google's image server for a 544px square instead of the
// small list thumbnail.
var sizeRe = regexp.MustCompile(`=w\d+-h\d+[^/]*$`)

func bigger(u string) string { return sizeRe.ReplaceAllString(u, "=w544-h544-l90-rj") }

func seconds(s string) int {
	total := 0
	for _, p := range strings.Split(s, ":") {
		n, _ := strconv.Atoi(p)
		total = total*60 + n
	}
	return total
}

func runs(col any) []map[string]any {
	list, _ := dig(col, "musicResponsiveListItemFlexColumnRenderer", "text", "runs").([]any)
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func dig(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func str(v any, path ...string) string {
	s, _ := dig(v, path...).(string)
	return s
}
