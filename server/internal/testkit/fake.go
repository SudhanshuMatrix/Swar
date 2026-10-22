package testkit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"swar/internal/catalog"
)

// Fake is an in-memory catalog provider with call counters and failure injection for tests.
// It requires NO network sockets, ensuring tests run fully isolated inside the sandbox.
type Fake struct {
	Calls atomic.Int64

	mu        sync.Mutex
	byCall    map[string]int
	failNext  map[string][]int
	delay     map[string]time.Duration
	override  map[string]any
	mediaURLs bool
	downloads bool
}

func NewFake(o Options) *Fake {
	return &Fake{
		byCall:    make(map[string]int),
		failNext:  make(map[string][]int),
		delay:     make(map[string]time.Duration),
		override:  make(map[string]any),
		mediaURLs: o.MediaURLs,
		downloads: o.Downloads,
	}
}

func (f *Fake) Close() {}

func (f *Fake) APIURL() string {
	return "http://fake.catalog.local/api.php"
}

func (f *Fake) Count(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byCall[call]
}

func (f *Fake) FailNext(call string, codes ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext[call] = append(f.failNext[call], codes...)
}

func (f *Fake) Delay(call string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delay[call] = d
}

func (f *Fake) Override(call string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.override[call] = v
}

func (f *Fake) checkCall(call string) error {
	f.Calls.Add(1)
	f.mu.Lock()
	f.byCall[call]++
	var code int
	if fails := f.failNext[call]; len(fails) > 0 {
		code = fails[0]
		f.failNext[call] = fails[1:]
	}
	d := f.delay[call]
	f.mu.Unlock()

	if d > 0 {
		time.Sleep(d)
	}

	if code != 0 {
		if code == 429 {
			return catalog.NewError(catalog.KindRateLimited, "rate limited", nil)
		}
		if code == 404 {
			return catalog.NotFound("not found")
		}
		return catalog.NewError(catalog.KindUpstream, fmt.Sprintf("upstream returned %d", code), nil)
	}
	return nil
}

func (f *Fake) Name() string {
	return "saavn"
}

func (f *Fake) Supports(provider string) bool {
	return true
}

func makeFakeSong(id, title, artistID, artistName, albumID, albumName string, durSec int, downloads bool) catalog.Track {
	cleanID := strings.TrimPrefix(id, "saavn:")
	return catalog.Track{
		ID:         catalog.MakeID("saavn", cleanID),
		Provider:   "saavn",
		ProviderID: cleanID,
		Title:      title,
		Artists: []catalog.ArtistRef{
			{
				ID:   catalog.MakeID("saavn", artistID),
				Name: artistName,
				Role: "singer",
			},
		},
		Album: &catalog.AlbumRef{
			ID:   catalog.MakeID("saavn", albumID),
			Name: albumName,
		},
		DurationSec: durSec,
		Images: []catalog.Image{
			{Quality: "150x150", URL: "http://c.saavncdn.com/" + cleanID + "-150x150.jpg"},
		},
		Language:    "hindi",
		Year:        2022,
		ReleaseDate: "2022-07-17",
		PlayCount:   12345,
		Playable:    true,
		Download: catalog.DownloadEligibility{
			Eligible: downloads,
			Reason:   map[bool]string{true: "operator_permitted", false: "downloads_not_enabled_for_source"}[downloads],
		},
		ProviderMeta: map[string]string{
			"lyrics_id": cleanID,
		},
	}
}

func (f *Fake) SearchSongs(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Track], error) {
	if err := f.checkCall("search.getResults"); err != nil {
		return nil, err
	}
	s1 := makeFakeSong("s1", "Kesariya", "a1", "Arijit Singh", "al1", "Brahmastra", 268, f.downloads)
	s2 := makeFakeSong("s2", "Kesariya (Remix)", "a1", "Arijit Singh", "al1", "Brahmastra", 200, f.downloads)
	s1dup := makeFakeSong("s1dup", "Kesariya", "a1", "Arijit Singh", "al1", "Brahmastra", 269, f.downloads)
	res := []catalog.Track{s1, s2, s1dup}
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	return &catalog.Page[catalog.Track]{Total: 3, Start: 1, Page: page, Limit: limit, Results: res}, nil
}

func (f *Fake) SearchAlbums(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Album], error) {
	if err := f.checkCall("search.getAlbumResults"); err != nil {
		return nil, err
	}
	res := []catalog.Album{
		{
			ID:         "saavn:al1",
			Provider:   "saavn",
			ProviderID: "al1",
			Name:       "Brahmastra",
			SongCount:  10,
			Year:       2022,
		},
	}
	return &catalog.Page[catalog.Album]{Total: 1, Start: 1, Page: page, Limit: limit, Results: res}, nil
}

func (f *Fake) SearchArtists(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.ArtistRef], error) {
	if err := f.checkCall("search.getArtistResults"); err != nil {
		return nil, err
	}
	cnt := 25
	res := []catalog.ArtistRef{
		{
			ID:        "saavn:a1",
			Name:      "Arijit Singh",
			URL:       "https://www.jiosaavn.com/artist/arijit-singh/a1",
			SongCount: &cnt,
		},
	}
	return &catalog.Page[catalog.ArtistRef]{Total: 1, Start: 1, Page: page, Limit: limit, Results: res}, nil
}

func (f *Fake) SearchPlaylists(ctx context.Context, q string, page, limit int) (*catalog.Page[catalog.Playlist], error) {
	if err := f.checkCall("search.getPlaylistResults"); err != nil {
		return nil, err
	}
	res := []catalog.Playlist{
		{
			ID:         "saavn:p1",
			Provider:   "saavn",
			ProviderID: "p1",
			Name:       "Romantic Hits",
			SongCount:  25,
			Language:   "hindi",
		},
	}
	return &catalog.Page[catalog.Playlist]{Total: 1, Start: 1, Page: page, Limit: limit, Results: res}, nil
}

func (f *Fake) Tracks(ctx context.Context, ids []string) ([]catalog.Track, error) {
	if err := f.checkCall("song.getDetails"); err != nil {
		return nil, err
	}
	var out []catalog.Track
	for _, id := range ids {
		clean := strings.TrimPrefix(id, "saavn:")
		if clean == "" || clean == "missing" {
			continue
		}
		switch clean {
		case "s1":
			out = append(out, makeFakeSong("s1", "Kesariya", "a2", "Arijit Singh", "al1", "Brahmastra", 268, f.downloads))
		case "s2":
			out = append(out, makeFakeSong("s2", "Kesariya (Remix)", "a1", "Arijit Singh", "al1", "Brahmastra", 200, f.downloads))
		case "r1":
			out = append(out, makeFakeSong("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262, f.downloads))
		case "r2":
			out = append(out, makeFakeSong("r2", "Channa Mereya", "a1", "Arijit Singh", "al4", "ADHM", 289, f.downloads))
		case "r3":
			out = append(out, makeFakeSong("r3", "Raabta", "a3", "Pritam", "al5", "Agent Vinod", 240, f.downloads))
		default:
			out = append(out, makeFakeSong(clean, "Song "+clean, "a1", "Arijit Singh", "al1", "Album "+clean, 200, f.downloads))
		}
	}
	return out, nil
}

func (f *Fake) TrackByLink(ctx context.Context, token string) ([]catalog.Track, error) {
	return f.Tracks(ctx, []string{token})
}

func (f *Fake) Related(ctx context.Context, id string, limit int) ([]catalog.Track, error) {
	if err := f.checkCall("webradio.createEntityStation"); err != nil {
		return nil, err
	}
	if err := f.checkCall("webradio.getSong"); err != nil {
		return nil, err
	}
	return []catalog.Track{
		makeFakeSong("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262, f.downloads),
		makeFakeSong("r2", "Channa Mereya", "a1", "Arijit Singh", "al4", "ADHM", 289, f.downloads),
		makeFakeSong("r3", "Raabta", "a3", "Pritam", "al5", "Agent Vinod", 240, f.downloads),
	}, nil
}

func (f *Fake) Album(ctx context.Context, id string) (*catalog.Album, error) {
	if err := f.checkCall("content.getAlbumDetails"); err != nil {
		return nil, err
	}
	clean := strings.TrimPrefix(id, "saavn:")
	t1 := makeFakeSong("t1", "Track One", "a1", "Arijit Singh", clean, "Album "+clean, 200, f.downloads)
	t2 := makeFakeSong("t2", "Track Two", "a1", "Arijit Singh", clean, "Album "+clean, 210, f.downloads)
	return &catalog.Album{
		ID:         catalog.MakeID("saavn", clean),
		Provider:   "saavn",
		ProviderID: clean,
		Name:       "Album " + clean,
		SongCount:  2,
		Tracks:     []catalog.Track{t1, t2},
	}, nil
}

func (f *Fake) AlbumByLink(ctx context.Context, token string) (*catalog.Album, error) {
	return f.Album(ctx, token)
}

func (f *Fake) Artist(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Artist, error) {
	if err := f.checkCall("artist.getArtistPageDetails"); err != nil {
		return nil, err
	}
	clean := strings.TrimPrefix(id, "saavn:")
	top1 := makeFakeSong("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262, f.downloads)
	top2 := makeFakeSong("x9", "Ae Dil", "a1", "Arijit Singh", "al9", "ADHM", 250, f.downloads)
	return &catalog.Artist{
		ID:            catalog.MakeID("saavn", clean),
		Provider:      "saavn",
		ProviderID:    clean,
		Name:          "Arijit Singh",
		FollowerCount: 1000,
		IsVerified:    true,
		TopSongs:      []catalog.Track{top1, top2},
		TopAlbums: []catalog.Album{
			{ID: "saavn:al3", Name: "Aashiqui 2"},
		},
		SimilarArtists: []catalog.ArtistRef{
			{ID: "saavn:a3", Name: "Pritam"},
		},
	}, nil
}

func (f *Fake) ArtistByLink(ctx context.Context, token string, o catalog.ArtistOptions) (*catalog.Artist, error) {
	return f.Artist(ctx, token, o)
}

func (f *Fake) ArtistSongs(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Page[catalog.Track], error) {
	if err := f.checkCall("artist.getArtistMoreSong"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	ov, hasOv := f.override["artist.getArtistMoreSong"]
	f.mu.Unlock()
	if hasOv {
		if m, ok := ov.(map[string]any); ok {
			if ts, ok := m["topSongs"].(map[string]any); ok {
				if total, ok := ts["total"].(int); ok && total == 0 {
					return &catalog.Page[catalog.Track]{Total: 0, Results: []catalog.Track{}}, nil
				}
			}
		}
	}
	top1 := makeFakeSong("r1", "Tum Hi Ho", "a1", "Arijit Singh", "al3", "Aashiqui 2", 262, f.downloads)
	top2 := makeFakeSong("x9", "Ae Dil", "a1", "Arijit Singh", "al9", "ADHM", 250, f.downloads)
	return &catalog.Page[catalog.Track]{Total: 2, Page: o.Page, Limit: 25, Results: []catalog.Track{top1, top2}}, nil
}

func (f *Fake) ArtistAlbums(ctx context.Context, id string, o catalog.ArtistOptions) (*catalog.Page[catalog.Album], error) {
	if err := f.checkCall("artist.getArtistMoreAlbum"); err != nil {
		return nil, err
	}
	return &catalog.Page[catalog.Album]{Total: 1, Page: o.Page, Limit: 25, Results: []catalog.Album{{ID: "saavn:al3", Name: "Aashiqui 2"}}}, nil
}

func (f *Fake) Playlist(ctx context.Context, id string, page, limit int) (*catalog.Playlist, error) {
	if err := f.checkCall("playlist.getDetails"); err != nil {
		return nil, err
	}
	clean := strings.TrimPrefix(id, "saavn:")
	t1 := makeFakeSong("t1", "Track One", "a1", "Arijit Singh", "al1", "A", 200, f.downloads)
	t2 := makeFakeSong("t2", "Track Two", "a2", "Pritam", "al2", "B", 210, f.downloads)
	return &catalog.Playlist{
		ID:         catalog.MakeID("saavn", clean),
		Provider:   "saavn",
		ProviderID: clean,
		Name:       "Playlist " + clean,
		SongCount:  2,
		Tracks:     []catalog.Track{t1, t2},
	}, nil
}

func (f *Fake) PlaylistByLink(ctx context.Context, token string, page, limit int) (*catalog.Playlist, error) {
	return f.Playlist(ctx, token, page, limit)
}

func (f *Fake) Lyrics(ctx context.Context, id string) (*catalog.Lyrics, error) {
	if err := f.checkCall("lyrics.getLyrics"); err != nil {
		return nil, err
	}
	clean := strings.TrimPrefix(id, "saavn:")
	return &catalog.Lyrics{
		TrackID:   catalog.MakeID("saavn", clean),
		Text:      "line one\nline two",
		Snippet:   "line one",
		Copyright: "(c) Label",
	}, nil
}

func (f *Fake) Trending(ctx context.Context, language string) ([]catalog.Collection, error) {
	if err := f.checkCall("content.getTrending"); err != nil {
		return nil, err
	}
	return []catalog.Collection{
		{
			Key:   "trending",
			Title: "Trending",
			Items: []catalog.Item{
				{Type: "song", ID: "saavn:s1", Title: "Kesariya"},
			},
		},
	}, nil
}

func (f *Fake) Browse(ctx context.Context, language string) ([]catalog.Collection, error) {
	if err := f.checkCall("content.getBrowseModules"); err != nil {
		return nil, err
	}
	return []catalog.Collection{
		{
			Key:   "browse",
			Title: "New Albums",
			Items: []catalog.Item{
				{Type: "album", ID: "saavn:al1", Title: "Brahmastra"},
			},
		},
	}, nil
}

func (f *Fake) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	if !f.mediaURLs {
		return nil, time.Time{}, catalog.Unavailable("streaming for this source is not enabled on this server")
	}
	if err := f.checkCall("song.getDetails"); err != nil {
		return nil, time.Time{}, err
	}
	clean := strings.TrimPrefix(id, "saavn:")
	expiry := time.Now().Add(3 * time.Hour)
	sources := []catalog.MediaSource{
		{Kind: "audio", Quality: "12kbps", URL: "https://aac.saavncdn.com/" + clean + "_12.mp4", Bitrate: 12, MimeType: "audio/mp4"},
		{Kind: "audio", Quality: "48kbps", URL: "https://aac.saavncdn.com/" + clean + "_48.mp4", Bitrate: 48, MimeType: "audio/mp4"},
		{Kind: "audio", Quality: "96kbps", URL: "https://aac.saavncdn.com/" + clean + "_96.mp4", Bitrate: 96, MimeType: "audio/mp4"},
		{Kind: "audio", Quality: "160kbps", URL: "https://aac.saavncdn.com/" + clean + "_160.mp4", Bitrate: 160, MimeType: "audio/mp4"},
		{Kind: "audio", Quality: "320kbps", URL: "https://aac.saavncdn.com/" + clean + "_320.mp4", Bitrate: 320, MimeType: "audio/mp4"},
	}
	return sources, expiry, nil
}
