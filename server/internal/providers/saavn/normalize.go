package saavn

import (
	"encoding/json"
	"html"
	"regexp"
	"sort"
	"strings"

	"swar/internal/catalog"
)

var imageSizeRe = regexp.MustCompile(`150x150|50x50`)

// images expands one artwork URL into the standard size ladder, smallest
// first, always over https.
func images(link fstr) []catalog.Image {
	l := strings.TrimSpace(string(link))
	if l == "" {
		return []catalog.Image{}
	}
	if strings.HasPrefix(l, "http://") {
		l = "https://" + strings.TrimPrefix(l, "http://")
	}
	if !strings.HasPrefix(l, "https://") {
		return []catalog.Image{}
	}
	sizes := []string{"50x50", "150x150", "500x500"}
	out := make([]catalog.Image, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, catalog.Image{Quality: s, URL: imageSizeRe.ReplaceAllString(l, s)})
	}
	return out
}

func text(s fstr) string { return html.UnescapeString(strings.TrimSpace(string(s))) }

func artistRef(a rawArtistMapEntry, role string) catalog.ArtistRef {
	if role == "" {
		role = text(a.Role)
	}
	return catalog.ArtistRef{
		ID:    catalog.MakeID(catalog.ProviderSaavn, string(a.ID)),
		Name:  text(a.Name),
		Role:  role,
		URL:   string(a.PermaURL),
		Image: images(a.Image),
	}
}

func artistsFromMap(m rawArtistMap, fallbackNames ...fstr) []catalog.ArtistRef {
	out := []catalog.ArtistRef{}
	for _, a := range m.Primary {
		out = append(out, artistRef(a, "primary"))
	}
	for _, a := range m.Featured {
		out = append(out, artistRef(a, "featured"))
	}
	if len(out) == 0 {
		for _, a := range m.All {
			out = append(out, artistRef(a, ""))
		}
	}
	if len(out) == 0 {
		for _, f := range fallbackNames {
			for _, n := range strings.Split(text(f), ",") {
				if n = strings.TrimSpace(n); n != "" {
					out = append(out, catalog.ArtistRef{Name: n, Role: "primary", Image: []catalog.Image{}})
				}
			}
			if len(out) > 0 {
				break
			}
		}
	}
	return out
}

func (c *Client) track(s rawSong) catalog.Track {
	mi := s.MoreInfo.V
	t := catalog.Track{
		ID:          catalog.MakeID(catalog.ProviderSaavn, string(s.ID)),
		Provider:    catalog.ProviderSaavn,
		ProviderID:  string(s.ID),
		Title:       text(s.Title),
		Artists:     artistsFromMap(mi.ArtistMap.V, mi.PrimaryArtists, mi.Singers, s.Subtitle),
		DurationSec: mi.Duration.Int(),
		Images:      images(s.Image),
		Language:    firstNonEmpty(text(s.Language), text(mi.Language)),
		Year:        firstNonZero(s.Year.Int(), mi.Year.Int()),
		ReleaseDate: text(mi.ReleaseDate),
		Explicit:    s.ExplicitContent.Bool(),
		PlayCount:   s.PlayCount.Int64(),
		Label:       text(mi.Label),
		Copyright:   text(mi.CopyrightText),
		HasLyrics:   mi.HasLyrics.Bool(),
		URL:         string(s.PermaURL),
	}
	if mi.AlbumID != "" || mi.Album != "" {
		t.Album = &catalog.AlbumRef{
			ID:   catalog.MakeID(catalog.ProviderSaavn, string(mi.AlbumID)),
			Name: text(mi.Album),
			URL:  string(mi.AlbumURL),
		}
	}
	meta := map[string]string{}
	if mi.LyricsID != "" {
		meta["lyricsId"] = string(mi.LyricsID)
	}
	if mi.KBPS320.Bool() {
		meta["has320kbps"] = "true"
	}
	if len(meta) > 0 {
		t.ProviderMeta = meta
	}

	hasMedia := mi.EncryptedMediaURL != ""
	t.Playable = hasMedia && c.mediaURLs
	t.Download = c.downloadEligibility(hasMedia, mi.Rights.V)
	return t
}

func (c *Client) downloadEligibility(hasMedia bool, r rawRights) catalog.DownloadEligibility {
	switch {
	case !hasMedia:
		return catalog.DownloadEligibility{Reason: "no_media_reference"}
	case !c.downloads || !c.mediaURLs:
		return catalog.DownloadEligibility{Reason: "downloads_not_enabled_for_source"}
	case r.Cacheable != "" && !r.Cacheable.Bool():
		return catalog.DownloadEligibility{Reason: "provider_rights_disallow"}
	}
	return catalog.DownloadEligibility{Eligible: true, Reason: "operator_permitted"}
}

func (c *Client) tracks(in []rawSong) []catalog.Track {
	out := make([]catalog.Track, 0, len(in))
	for _, s := range in {
		if s.ID == "" {
			continue
		}
		out = append(out, c.track(s))
	}
	return out
}

func (c *Client) album(a rawAlbum) catalog.Album {
	mi := a.MoreInfo.V
	return catalog.Album{
		ID:          catalog.MakeID(catalog.ProviderSaavn, string(a.ID)),
		Provider:    catalog.ProviderSaavn,
		ProviderID:  string(a.ID),
		Name:        text(a.Title),
		Description: text(firstNonEmptyF(a.HeaderDesc, a.Description)),
		Year:        firstNonZero(a.Year.Int(), mi.Year.Int()),
		Language:    firstNonEmpty(text(a.Language), text(mi.Language)),
		Explicit:    a.ExplicitContent.Bool(),
		PlayCount:   a.PlayCount.Int64(),
		URL:         string(a.PermaURL),
		SongCount:   firstNonZero(mi.SongCount.Int(), len(a.List)),
		Artists:     artistsFromMap(mi.ArtistMap.V, mi.Music, a.Subtitle),
		Images:      images(a.Image),
		Tracks:      c.tracks(a.List),
	}
}

func (c *Client) playlist(p rawPlaylist) catalog.Playlist {
	mi := p.MoreInfo.V
	artists := make([]catalog.ArtistRef, 0, len(mi.Artists))
	for _, a := range mi.Artists {
		artists = append(artists, artistRef(a, ""))
	}
	return catalog.Playlist{
		ID:          catalog.MakeID(catalog.ProviderSaavn, string(p.ID)),
		Provider:    catalog.ProviderSaavn,
		ProviderID:  string(p.ID),
		Name:        text(p.Title),
		Description: text(firstNonEmptyF(p.HeaderDesc, p.Description)),
		Language:    firstNonEmpty(text(p.Language), text(mi.Language)),
		Explicit:    p.ExplicitContent.Bool(),
		URL:         string(p.PermaURL),
		SongCount:   firstNonZero(p.ListCount.Int(), mi.SongCount.Int(), len(p.List)),
		PlayCount:   p.PlayCount.Int64(),
		Artists:     artists,
		Images:      images(p.Image),
		Tracks:      c.tracks(p.List),
	}
}

func (c *Client) artist(a rawArtist) catalog.Artist {
	id := firstNonEmptyF(a.ArtistID, a.ID)
	out := catalog.Artist{
		ID:               catalog.MakeID(catalog.ProviderSaavn, string(id)),
		Provider:         catalog.ProviderSaavn,
		ProviderID:       string(id),
		Name:             text(a.Name),
		URL:              string(firstNonEmptyF(a.URLs.V.Overview, a.PermaURL)),
		Images:           images(a.Image),
		FollowerCount:    a.FollowerCount.Int64(),
		IsVerified:       a.IsVerified.Bool(),
		DominantLanguage: text(a.DominantLanguage),
		DominantType:     text(a.DominantType),
		Bio:              parseBio(string(a.Bio)),
		TopSongs:         c.tracks(a.TopSongs),
		Singles:          c.tracks(a.Singles),
		IsRadioPresent:   a.IsRadioPresent.Bool(),
	}
	for _, al := range a.TopAlbums {
		if al.ID != "" {
			out.TopAlbums = append(out.TopAlbums, c.album(al))
		}
	}
	for _, s := range a.SimilarArtists {
		if s.ID == "" {
			continue
		}
		out.SimilarArtists = append(out.SimilarArtists, catalog.ArtistRef{
			ID:    catalog.MakeID(catalog.ProviderSaavn, string(s.ID)),
			Name:  text(s.Name),
			URL:   string(s.PermaURL),
			Image: images(s.ImageURL),
		})
	}
	return out
}

// parseBio decodes the JSON-encoded bio string; invalid input yields nil.
func parseBio(raw string) []catalog.BioPart {
	if raw == "" {
		return nil
	}
	var parts []struct {
		Text     fstr `json:"text"`
		Title    fstr `json:"title"`
		Sequence fstr `json:"sequence"`
	}
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return nil
	}
	out := make([]catalog.BioPart, 0, len(parts))
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		out = append(out, catalog.BioPart{Title: text(p.Title), Text: text(p.Text), Sequence: p.Sequence.Int()})
	}
	return out
}

type rawItem struct {
	ID       fstr             `json:"id"`
	Title    fstr             `json:"title"`
	Name     fstr             `json:"name"`
	Subtitle fstr             `json:"subtitle"`
	Type     fstr             `json:"type"`
	Image    fstr             `json:"image"`
	PermaURL fstr             `json:"perma_url"`
	Language fstr             `json:"language"`
	MoreInfo obj[rawSongMore] `json:"more_info"`
}

func item(r rawItem) (catalog.Item, bool) {
	if r.ID == "" {
		return catalog.Item{}, false
	}
	title := firstNonEmptyF(r.Title, r.Name)
	if title == "" {
		return catalog.Item{}, false
	}
	typ := text(r.Type)
	if typ == "" {
		typ = "unknown"
	}
	return catalog.Item{
		Type:     typ,
		ID:       catalog.MakeID(catalog.ProviderSaavn, string(r.ID)),
		Title:    text(title),
		Subtitle: text(r.Subtitle),
		URL:      string(r.PermaURL),
		Images:   images(r.Image),
		Language: firstNonEmpty(text(r.Language), text(r.MoreInfo.V.Language)),
	}, true
}

// collections tolerantly converts an unverified browse/trending payload
// (either an array of items or an object of arrays) into collections.
func collections(raw json.RawMessage, defaultKey string) []catalog.Collection {
	var arr list[rawItem]
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		c := catalog.Collection{Key: defaultKey, Title: humanize(defaultKey), Items: []catalog.Item{}}
		for _, r := range arr {
			if it, ok := item(r); ok {
				c.Items = append(c.Items, it)
			}
		}
		if len(c.Items) == 0 {
			return []catalog.Collection{}
		}
		return []catalog.Collection{c}
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return []catalog.Collection{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []catalog.Collection{}
	for _, k := range keys {
		var items list[rawItem]
		if err := json.Unmarshal(m[k], &items); err != nil || len(items) == 0 {
			continue
		}
		c := catalog.Collection{Key: k, Title: humanize(k), Items: []catalog.Item{}}
		for _, r := range items {
			if it, ok := item(r); ok {
				c.Items = append(c.Items, it)
			}
		}
		if len(c.Items) > 0 {
			out = append(out, c)
		}
	}
	return out
}

func humanize(k string) string {
	k = strings.NewReplacer("_", " ", "-", " ").Replace(k)
	if k == "" {
		return k
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmptyF(v ...fstr) fstr {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstNonZero(v ...int) int {
	for _, n := range v {
		if n != 0 {
			return n
		}
	}
	return 0
}
