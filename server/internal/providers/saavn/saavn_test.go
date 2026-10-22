package saavn

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/providers/saavn/saavntest"
)

func newClient(t *testing.T, f *saavntest.Fake, mod func(*Options)) *Client {
	t.Helper()
	o := Options{BaseURL: f.APIURL(), Timeout: 2 * time.Second, MaxConcurrency: 4, MaxRetries: 2, MaxBodyBytes: 1 << 20}
	if mod != nil {
		mod(&o)
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() } // no real backoff in tests
	return c
}

func TestSearchSongsNormalizes(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, nil)
	p, err := c.SearchSongs(context.Background(), "kesariya", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 3 || p.Start != 1 || len(p.Results) != 3 {
		t.Fatalf("unexpected page: total=%d start=%d n=%d", p.Total, p.Start, len(p.Results))
	}
	tr := p.Results[0]
	if tr.ID != "saavn:s1" || tr.Provider != "saavn" || tr.ProviderID != "s1" {
		t.Errorf("ids: %+v", tr)
	}
	if tr.Title != "Kesariya" || tr.DurationSec != 268 || tr.Year != 2022 || tr.Language != "hindi" {
		t.Errorf("fields: %+v", tr)
	}
	if tr.ArtistNames() != "Arijit Singh" || tr.Artists[0].ID != "saavn:a1" {
		t.Errorf("artists: %+v", tr.Artists)
	}
	if tr.Album == nil || tr.Album.ID != "saavn:al1" || tr.Album.Name != "Brahmastra" {
		t.Errorf("album: %+v", tr.Album)
	}
	if len(tr.Images) != 3 || tr.Images[2].Quality != "500x500" || !strings.HasPrefix(tr.Images[2].URL, "https://") || !strings.Contains(tr.Images[2].URL, "500x500") {
		t.Errorf("images: %+v", tr.Images)
	}
	if !tr.HasLyrics || tr.ProviderMeta["lyricsId"] != "s1" {
		t.Errorf("lyrics meta: %+v", tr.ProviderMeta)
	}
	// Media resolution and downloads are off by default.
	if tr.Playable || tr.Download.Eligible || tr.Download.Reason != "downloads_not_enabled_for_source" {
		t.Errorf("defaults must not claim playback/download: playable=%v dl=%+v", tr.Playable, tr.Download)
	}
}

func TestMissingAndOddFieldsDoNotFail(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	f.Override("search.getResults", map[string]any{
		"total": nil, "start": "x",
		"results": []any{
			// more_info is a string, artistMap missing, numbers as numbers.
			map[string]any{"id": "o1", "title": "Odd &amp; Song", "more_info": "", "year": 2020, "play_count": 7},
			// Entirely the wrong type: skipped, not fatal.
			"garbage",
			// No ID: skipped.
			map[string]any{"title": "No ID"},
			map[string]any{"id": "o2", "title": "Arrays", "more_info": map[string]any{"artistMap": []any{}, "duration": 99, "primary_artists": "A, B"}, "image": ""},
		},
	})
	c := newClient(t, f, nil)
	p, err := c.SearchSongs(context.Background(), "x", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Results) != 2 {
		t.Fatalf("want 2 tolerant results, got %d", len(p.Results))
	}
	if p.Results[0].Title != "Odd & Song" || p.Results[0].Year != 2020 || p.Results[0].PlayCount != 7 {
		t.Errorf("odd song: %+v", p.Results[0])
	}
	if p.Results[1].DurationSec != 99 || p.Results[1].ArtistNames() != "A, B" || len(p.Results[1].Images) != 0 {
		t.Errorf("fallback artists/duration: %+v", p.Results[1])
	}
}

func TestTimeoutIsTypedAndBounded(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	f.Delay("search.getResults", 500*time.Millisecond)
	c := newClient(t, f, func(o *Options) { o.Timeout = 50 * time.Millisecond; o.MaxRetries = 1 })
	start := time.Now()
	_, err := c.SearchSongs(context.Background(), "x", 1, 5)
	if catalog.KindOf(err) != catalog.KindTimeout {
		t.Fatalf("want timeout, got %v", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Errorf("timeout not enforced: took %v", time.Since(start))
	}
	if n := f.Count("search.getResults"); n != 2 {
		t.Errorf("want 1 retry (2 calls), got %d", n)
	}
}

func TestRateLimitRetriesThenFails(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	f.FailNext("song.getDetails", 429, 429, 429)
	c := newClient(t, f, nil)
	_, err := c.Tracks(context.Background(), []string{"s1"})
	if catalog.KindOf(err) != catalog.KindRateLimited {
		t.Fatalf("want rate_limited, got %v", err)
	}
	if n := f.Count("song.getDetails"); n != 3 {
		t.Errorf("want 3 attempts, got %d", n)
	}
}

func TestTransientErrorRecovers(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	f.FailNext("song.getDetails", 502)
	c := newClient(t, f, nil)
	ts, err := c.Tracks(context.Background(), []string{"s1"})
	if err != nil || len(ts) != 1 {
		t.Fatalf("want recovery after one 502, got %v", err)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	f.FailNext("song.getDetails", 403)
	c := newClient(t, f, nil)
	_, err := c.Tracks(context.Background(), []string{"s1"})
	if catalog.KindOf(err) != catalog.KindUpstream || f.Count("song.getDetails") != 1 {
		t.Fatalf("4xx must fail fast: %v (calls=%d)", err, f.Count("song.getDetails"))
	}
}

func TestResponseSizeLimit(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	f.Override("search.getResults", map[string]any{"results": []any{}, "pad": strings.Repeat("x", 4096)})
	c := newClient(t, f, func(o *Options) { o.MaxBodyBytes = 1024 })
	_, err := c.SearchSongs(context.Background(), "x", 1, 5)
	if catalog.KindOf(err) != catalog.KindUpstream || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size-limit error, got %v", err)
	}
}

func TestNotFound(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, nil)
	if _, err := c.Tracks(context.Background(), []string{"missing"}); catalog.KindOf(err) != catalog.KindNotFound {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestMediaSourcesDisabledByDefault(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, nil)
	_, _, err := c.MediaSources(context.Background(), "s1")
	if catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
	if f.Calls.Load() != 0 {
		t.Error("disabled media resolution must not call upstream")
	}
}

func TestMediaSourcesWhenEnabled(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, func(o *Options) { o.MediaURLs = true })
	srcs, exp, err := c.MediaSources(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 5 || srcs[3].Quality != "160kbps" || srcs[3].URL != strings.Replace(saavntest.MediaURL("s1"), "_96", "_160", 1) {
		t.Fatalf("sources: %+v", srcs)
	}
	if time.Until(exp) <= 0 {
		t.Error("expiry must be in the future")
	}
}

func TestDownloadEligibilityRequiresOperatorOptIn(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, func(o *Options) { o.MediaURLs = true; o.Downloads = true })
	ts, _ := c.Tracks(context.Background(), []string{"s1"})
	if !ts[0].Download.Eligible || !ts[0].Playable {
		t.Fatalf("want eligible when operator opted in: %+v", ts[0].Download)
	}
	song := saavntest.Song("nr", "No Rights", "a", "A", "al", "Al", 100)
	song["more_info"].(map[string]any)["rights"] = map[string]any{"cacheable": "false"}
	f.Override("song.getDetails", map[string]any{"songs": []any{song}})
	ts, _ = c.Tracks(context.Background(), []string{"nr"})
	if ts[0].Download.Eligible || ts[0].Download.Reason != "provider_rights_disallow" {
		t.Fatalf("provider rights must be respected: %+v", ts[0].Download)
	}
}

func TestRelatedUsesStationAndSkipsSeed(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, nil)
	ts, err := c.Related(context.Background(), "r1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[0].ID != "saavn:r2" || ts[1].ID != "saavn:r3" {
		t.Fatalf("related: %+v", ts)
	}
}

func TestArtistBioAndLyricsAndCollections(t *testing.T) {
	f := saavntest.New()
	defer f.Close()
	c := newClient(t, f, nil)
	a, err := c.Artist(context.Background(), "a1", catalog.ArtistOptions{SongCount: 5})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "saavn:a1" || len(a.Bio) != 1 || a.Bio[0].Text != "Singer" || len(a.TopSongs) != 2 || len(a.Singles) != 0 || len(a.SimilarArtists) != 1 {
		t.Fatalf("artist: %+v", a)
	}
	l, err := c.Lyrics(context.Background(), "s1")
	if err != nil || l.Text != "line one\nline two" {
		t.Fatalf("lyrics: %+v %v", l, err)
	}
	tr, err := c.Trending(context.Background(), "hindi")
	if err != nil || len(tr) != 1 || tr[0].Items[0].ID != "saavn:s1" {
		t.Fatalf("trending: %+v %v", tr, err)
	}
	br, err := c.Browse(context.Background(), "hindi")
	if err != nil || len(br) != 1 || br[0].Key != "new_albums" {
		t.Fatalf("browse: %+v %v", br, err)
	}
}

func TestFstrDecoding(t *testing.T) {
	var v struct {
		A, B, C, D, E fstr
	}
	if err := json.Unmarshal([]byte(`{"A":"x","B":12,"C":true,"D":null,"E":{"k":1}}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != "x" || v.B.Int() != 12 || !v.C.Bool() || v.D != "" || v.E != "" {
		t.Fatalf("%+v", v)
	}
}

func TestBadBaseURLRejected(t *testing.T) {
	for _, u := range []string{"", "ftp://x", "not a url", "https://"} {
		if _, err := New(Options{BaseURL: u}); err == nil {
			t.Errorf("%q should be rejected", u)
		}
	}
}

func TestRequestCarriesExpectedParams(t *testing.T) {
	var got string
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		w.Write([]byte(`{"songs":[]}`))
	})
	f := saavntest.New()
	f.Config.Handler = srv
	defer f.Close()
	c := newClient(t, f, nil)
	_, _ = c.Tracks(context.Background(), []string{"a", "b"})
	for _, want := range []string{"__call=song.getDetails", "_format=json", "api_version=4", "ctx=web6dot0", "pids=a%2Cb"} {
		if !strings.Contains(got, want) {
			t.Errorf("query %q missing %q", got, want)
		}
	}
}
