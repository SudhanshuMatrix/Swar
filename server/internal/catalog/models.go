// Package catalog holds Swar's provider-independent music models and the
// interface that catalog providers implement. Nothing in this package knows
// about Saavn's (or any other upstream's) response shapes.
package catalog

import (
	"strings"
)

// Provider names used as the prefix of internal IDs.
const (
	ProviderSaavn   = "saavn"
	ProviderYouTube = "yt"
)

// MakeID builds Swar's internal ID: "<provider>:<providerID>".
func MakeID(provider, providerID string) string {
	if providerID == "" {
		return ""
	}
	return provider + ":" + providerID
}

// ParseID splits an internal ID. Bare IDs (no prefix) are accepted for
// backwards compatibility with the original Flutter client, which used
// YouTube video IDs directly: an 11 character ID is treated as YouTube and
// anything else as Saavn.
func ParseID(id string) (provider, providerID string, ok bool) {
	id = strings.TrimSpace(id)
	if p, rest, found := strings.Cut(id, ":"); found {
		switch p {
		case ProviderSaavn, ProviderYouTube:
			if validProviderID(rest) {
				return p, rest, true
			}
		}
		return "", "", false
	}
	if !validProviderID(id) {
		return "", "", false
	}
	if IsYouTubeID(id) {
		return ProviderYouTube, id, true
	}
	return ProviderSaavn, id, true
}

// IsYouTubeID reports whether s looks like an 11-character YouTube video ID.
func IsYouTubeID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validProviderID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

type Image struct {
	Quality string `json:"quality"`
	URL     string `json:"url"`
}

type ArtistRef struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Role  string  `json:"role,omitempty"`
	URL   string  `json:"url,omitempty"`
	Image []Image `json:"image"`
	// SongCount is filled by artist search; nil means it was not looked up.
	SongCount *int `json:"songCount,omitempty"`
}

type AlbumRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// DownloadEligibility says whether a client may keep an offline copy of a
// track. Eligible is only true when the operator has asserted rights for
// the source and the provider metadata does not forbid it.
type DownloadEligibility struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason"`
}

type Track struct {
	ID          string              `json:"id"`
	Provider    string              `json:"provider"`
	ProviderID  string              `json:"providerId"`
	Title       string              `json:"title"`
	Artists     []ArtistRef         `json:"artists"`
	Album       *AlbumRef           `json:"album,omitempty"`
	DurationSec int                 `json:"durationSec"`
	Images      []Image             `json:"images"`
	Language    string              `json:"language,omitempty"`
	Genre       string              `json:"genre,omitempty"`
	Year        int                 `json:"year,omitempty"`
	ReleaseDate string              `json:"releaseDate,omitempty"`
	Explicit    bool                `json:"explicit"`
	PlayCount   int64               `json:"playCount,omitempty"`
	Label       string              `json:"label,omitempty"`
	Copyright   string              `json:"copyright,omitempty"`
	HasLyrics   bool                `json:"hasLyrics"`
	URL         string              `json:"url,omitempty"`
	Playable    bool                `json:"playable"`
	Download    DownloadEligibility `json:"download"`
	// ProviderMeta carries small provider-specific facts that are useful to
	// clients (e.g. "lyricsId", "channel"). Never secrets.
	ProviderMeta map[string]string `json:"providerMeta,omitempty"`
}

// ArtistNames joins the primary artists (or all artists) with ", ".
func (t *Track) ArtistNames() string {
	names := make([]string, 0, len(t.Artists))
	for _, a := range t.Artists {
		if a.Role == "" || a.Role == "primary_artists" || a.Role == "primary" || a.Role == "singer" {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		for _, a := range t.Artists {
			names = append(names, a.Name)
		}
	}
	return strings.Join(dedupeStrings(names), ", ")
}

// ArtistIDs returns the IDs of all credited artists.
func (t *Track) ArtistIDs() []string {
	out := make([]string, 0, len(t.Artists))
	for _, a := range t.Artists {
		if a.ID != "" {
			out = append(out, a.ID)
		}
	}
	return dedupeStrings(out)
}

// BestImage returns the largest image URL, or "".
func BestImage(images []Image) string {
	if len(images) == 0 {
		return ""
	}
	return images[len(images)-1].URL
}

type Album struct {
	ID          string      `json:"id"`
	Provider    string      `json:"provider"`
	ProviderID  string      `json:"providerId"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Year        int         `json:"year,omitempty"`
	Language    string      `json:"language,omitempty"`
	Explicit    bool        `json:"explicit"`
	PlayCount   int64       `json:"playCount,omitempty"`
	URL         string      `json:"url,omitempty"`
	SongCount   int         `json:"songCount,omitempty"`
	Artists     []ArtistRef `json:"artists"`
	Images      []Image     `json:"images"`
	Tracks      []Track     `json:"tracks,omitempty"`
}

type Artist struct {
	ID               string      `json:"id"`
	Provider         string      `json:"provider"`
	ProviderID       string      `json:"providerId"`
	Name             string      `json:"name"`
	URL              string      `json:"url,omitempty"`
	Images           []Image     `json:"images"`
	FollowerCount    int64       `json:"followerCount,omitempty"`
	IsVerified       bool        `json:"isVerified"`
	DominantLanguage string      `json:"dominantLanguage,omitempty"`
	DominantType     string      `json:"dominantType,omitempty"`
	Bio              []BioPart   `json:"bio,omitempty"`
	TopSongs         []Track     `json:"topSongs,omitempty"`
	TopAlbums        []Album     `json:"topAlbums,omitempty"`
	Singles          []Track     `json:"singles,omitempty"`
	SimilarArtists   []ArtistRef `json:"similarArtists,omitempty"`
	IsRadioPresent   bool        `json:"isRadioPresent"`
}

type BioPart struct {
	Title    string `json:"title,omitempty"`
	Text     string `json:"text"`
	Sequence int    `json:"sequence,omitempty"`
}

type Playlist struct {
	ID          string      `json:"id"`
	Provider    string      `json:"provider"`
	ProviderID  string      `json:"providerId"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Language    string      `json:"language,omitempty"`
	Explicit    bool        `json:"explicit"`
	URL         string      `json:"url,omitempty"`
	SongCount   int         `json:"songCount,omitempty"`
	PlayCount   int64       `json:"playCount,omitempty"`
	Artists     []ArtistRef `json:"artists,omitempty"`
	Images      []Image     `json:"images"`
	Tracks      []Track     `json:"tracks,omitempty"`
}

// Page is one page of results.
type Page[T any] struct {
	Total   int `json:"total"`
	Start   int `json:"start"`
	Page    int `json:"page"`
	Limit   int `json:"limit"`
	Results []T `json:"results"`
}

// Lyrics are only returned when the provider supplies them.
type Lyrics struct {
	TrackID   string `json:"trackId"`
	Text      string `json:"text"`
	Snippet   string `json:"snippet,omitempty"`
	Copyright string `json:"copyright,omitempty"`
}

// Item is a lightweight, typed reference used by browse modules.
type Item struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	URL      string  `json:"url,omitempty"`
	Images   []Image `json:"images"`
	Language string  `json:"language,omitempty"`
}

// Collection is a titled list of items (trending, charts, new releases...).
type Collection struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// MediaSource is a reference to an authorized media stream. The server
// never fetches these bytes; the client plays them directly.
type MediaSource struct {
	Kind     string `json:"kind"` // "audio" or "video"
	Quality  string `json:"quality"`
	URL      string `json:"url"`
	MimeType string `json:"mimeType,omitempty"`
	Bitrate  int    `json:"bitrateKbps,omitempty"`
}

type SortBy string

const (
	SortPopularity   SortBy = "popularity"
	SortLatest       SortBy = "latest"
	SortAlphabetical SortBy = "alphabetical"
)

type ArtistOptions struct {
	Page       int
	SongCount  int
	AlbumCount int
	SortBy     SortBy
	SortOrder  string // "asc" or "desc"
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
