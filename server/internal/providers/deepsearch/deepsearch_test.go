package deepsearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"swar/internal/catalog"
)

func TestParseInput(t *testing.T) {
	cases := []struct {
		in, query, id string
		invalid       bool
	}{
		{in: "kesariya arijit", query: "kesariya arijit"},
		{in: "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1", id: "dQw4w9WgXcQ"},
		{in: "https://youtu.be/dQw4w9WgXcQ", id: "dQw4w9WgXcQ"},
		{in: "music.youtube.com/watch?v=dQw4w9WgXcQ", id: "dQw4w9WgXcQ"},
		{in: "dQw4w9WgXcQ", id: "dQw4w9WgXcQ"},
		{in: "http://169.254.169.254/latest/meta-data", invalid: true},
		{in: "https://evil.example.com/watch?v=dQw4w9WgXcQ", invalid: true},
		{in: "file:///etc/passwd", invalid: true},
		{in: "www.example.com", invalid: true},
		{in: "", invalid: true},
		{in: "a", invalid: true},
	}
	for _, c := range cases {
		q, id, err := ParseInput(c.in)
		if c.invalid {
			if catalog.KindOf(err) != catalog.KindInvalid {
				t.Errorf("%q: want invalid, got q=%q id=%q err=%v", c.in, q, id, err)
			}
			continue
		}
		if err != nil || q != c.query || id != c.id {
			t.Errorf("%q: got q=%q id=%q err=%v", c.in, q, id, err)
		}
	}
}

type recorder struct {
	mu   sync.Mutex
	args [][]string
	out  []byte
	err  error
	wait time.Duration
}

func (r *recorder) run(ctx context.Context, args []string, max int64) ([]byte, error) {
	r.mu.Lock()
	r.args = append(r.args, args)
	r.mu.Unlock()
	if r.wait > 0 {
		select {
		case <-time.After(r.wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if int64(len(r.out)) > max {
		return nil, errOutputTooLarge
	}
	return r.out, r.err
}

func searchJSON() []byte {
	b, _ := json.Marshal(map[string]any{"entries": []any{
		map[string]any{"id": "AAAAAAAAAAA", "title": "Song A", "channel": "Chan A", "duration": 200.0},
		map[string]any{"id": "AAAAAAAAAAA", "title": "dup"},
		map[string]any{"id": "BBBBBBBBBBB", "title": "Live", "live_status": "is_live"},
		map[string]any{"id": "not-an-id", "title": "bad"},
		map[string]any{"id": "CCCCCCCCCCC", "title": "Song C", "uploader": "Up C", "thumbnails": []any{
			map[string]any{"url": "https://i.ytimg.com/c.jpg", "width": 480, "height": 360},
			map[string]any{"url": "http://insecure/c.jpg", "width": 900, "height": 900},
		}},
	}})
	return b
}

func TestSearchArgumentsAreSafe(t *testing.T) {
	r := &recorder{out: searchJSON()}
	s := New(Options{Runner: r.run, MaxResults: 5})
	res, err := s.Search(context.Background(), "--exec rm -rf / ; echo", 3)
	if err != nil {
		t.Fatal(err)
	}
	args := r.args[0]
	joined := strings.Join(args, " ")
	for _, want := range []string{"--ignore-config", "--skip-download", "--flat-playlist", "--no-playlist"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "ytsearch3:--exec rm -rf / ; echo" {
		t.Fatalf("query must be a single positional after --: %q", args[len(args)-2:])
	}
	// Dedupe, skip live/invalid entries, https-only thumbnails.
	if len(res) != 2 || res[0].ID != "yt:AAAAAAAAAAA" || res[1].ID != "yt:CCCCCCCCCCC" {
		t.Fatalf("results: %+v", res)
	}
	if res[0].ArtistNames() != "Chan A" || res[0].DurationSec != 200 || res[0].Confidence != "metadata_only" {
		t.Errorf("candidate: %+v", res[0])
	}
	for _, img := range res[1].Images {
		if !strings.HasPrefix(img.URL, "https://") {
			t.Errorf("insecure thumbnail leaked: %v", img)
		}
	}
	if res[0].Playable || res[0].Download.Eligible {
		t.Error("playback/download must be off by default")
	}
}

func TestVideoURLIsCanonicalized(t *testing.T) {
	r := &recorder{out: []byte(`{"id":"dQw4w9WgXcQ","title":"T"}`)}
	s := New(Options{Runner: r.run})
	if _, err := s.Search(context.Background(), "https://youtu.be/dQw4w9WgXcQ?si=tracking", 5); err != nil {
		t.Fatal(err)
	}
	if got := r.args[0][len(r.args[0])-1]; got != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("target = %q", got)
	}
}

func TestTimeoutAndConcurrencyLimits(t *testing.T) {
	r := &recorder{out: searchJSON(), wait: time.Second}
	s := New(Options{Runner: r.run, Timeout: 50 * time.Millisecond, Concurrency: 1})
	start := time.Now()
	_, err := s.Search(context.Background(), "slow", 3)
	if catalog.KindOf(err) != catalog.KindTimeout || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("want bounded timeout, got %v after %v", err, time.Since(start))
	}

	// With concurrency 1, a second caller waits and gives up with its ctx.
	var running atomic.Int32
	block := make(chan struct{})
	s2 := New(Options{Concurrency: 1, Timeout: time.Second, Runner: func(ctx context.Context, a []string, m int64) ([]byte, error) {
		running.Add(1)
		<-block
		return searchJSON(), nil
	}})
	go s2.Search(context.Background(), "first", 1)
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = s2.Search(ctx, "second", 1)
	close(block)
	if catalog.KindOf(err) != catalog.KindRateLimited || running.Load() != 1 {
		t.Fatalf("second search should be refused while busy: %v running=%d", err, running.Load())
	}
}

func TestOutputLimitAndMissingBinary(t *testing.T) {
	r := &recorder{out: searchJSON()}
	s := New(Options{Runner: r.run, MaxOutput: 10})
	if _, err := s.Search(context.Background(), "x y", 3); catalog.KindOf(err) != catalog.KindUpstream {
		t.Fatalf("want upstream error for oversized output, got %v", err)
	}
	s = New(Options{Binary: "definitely-not-yt-dlp-binary"})
	if _, err := s.Search(context.Background(), "x y", 3); catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("want unavailable when binary missing, got %v", err)
	}
}

func TestRestrictedErrors(t *testing.T) {
	r := &recorder{err: &exitError{err: errors.New("exit 1"), stderr: "ERROR: Private video. Sign in if you've been granted access"}}
	s := New(Options{Runner: r.run})
	if _, err := s.Search(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 1); catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestMediaSourcesGatedAndDRMSkipped(t *testing.T) {
	r := &recorder{}
	s := New(Options{Runner: r.run})
	if _, _, err := s.MediaSources(context.Background(), "dQw4w9WgXcQ"); catalog.KindOf(err) != catalog.KindUnavailable || len(r.args) != 0 {
		t.Fatalf("disabled playback must refuse without running yt-dlp: %v", err)
	}
	exp := time.Now().Add(2 * time.Hour).Unix()
	r.out, _ = json.Marshal(map[string]any{"id": "dQw4w9WgXcQ", "availability": "public", "formats": []any{
		map[string]any{"format_id": "drm", "url": "https://x/drm", "ext": "m4a", "acodec": "mp4a", "vcodec": "none", "abr": 999, "protocol": "https", "has_drm": true},
		map[string]any{"format_id": "a1", "url": "https://x/a1?expire=" + itoa(exp), "ext": "webm", "acodec": "opus", "vcodec": "none", "abr": 160, "protocol": "https"},
		map[string]any{"format_id": "a2", "url": "https://x/a2?expire=" + itoa(exp), "ext": "m4a", "acodec": "mp4a", "vcodec": "none", "abr": 128, "protocol": "https"},
		map[string]any{"format_id": "v1", "url": "https://x/v1", "ext": "mp4", "acodec": "none", "vcodec": "avc1", "height": 720, "protocol": "https"},
		map[string]any{"format_id": "v2", "url": "https://x/v2", "ext": "mp4", "acodec": "none", "vcodec": "avc1", "height": 1080, "protocol": "https"},
		map[string]any{"format_id": "hls", "url": "https://x/hls", "ext": "mp4", "acodec": "mp4a", "vcodec": "none", "abr": 500, "protocol": "m3u8_native"},
	}})
	s = New(Options{Runner: r.run, Playback: true})
	srcs, until, err := s.MediaSources(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 || srcs[0].URL[:11] != "https://x/a" || !strings.Contains(srcs[0].URL, "a2") || srcs[1].Quality != "720p" {
		t.Fatalf("sources: %+v", srcs)
	}
	if until.Unix() != exp {
		t.Errorf("expiry should come from the signed URL: %v", until)
	}
	r.out = []byte(`{"id":"dQw4w9WgXcQ","availability":"subscriber_only","formats":[]}`)
	if _, _, err := s.MediaSources(context.Background(), "dQw4w9WgXcQ"); catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("restricted content must be refused: %v", err)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestRestrictedVideoFallsBackToAnotherUpload(t *testing.T) {
	gated, _ := json.Marshal(map[string]any{"id": "AAAAAAAAAAA", "availability": "needs_auth", "formats": []any{}})
	search, _ := json.Marshal(map[string]any{"entries": []any{
		map[string]any{"id": "AAAAAAAAAAA", "title": "Peacock", "channel": "KatyPerryVEVO", "duration": 230},
		map[string]any{"id": "BBBBBBBBBBB", "title": "Peacock (Audio)", "channel": "Katy Perry", "duration": 231},
	}})
	ok, _ := json.Marshal(map[string]any{"id": "BBBBBBBBBBB", "availability": "public", "formats": []any{
		map[string]any{"format_id": "a", "url": "https://x/b", "ext": "m4a", "acodec": "mp4a", "vcodec": "none", "abr": 128, "protocol": "https"},
	}})
	var calls [][]string
	run := func(ctx context.Context, args []string, max int64) ([]byte, error) {
		calls = append(calls, args)
		target := args[len(args)-1]
		switch {
		case strings.HasPrefix(target, "ytsearch"):
			return search, nil
		case strings.HasSuffix(target, "AAAAAAAAAAA"):
			return gated, nil
		default:
			return ok, nil
		}
	}
	s := New(Options{Runner: run, Playback: true})
	if _, err := s.Search(context.Background(), "peacock katy perry", 5); err != nil {
		t.Fatal(err)
	}
	srcs, _, err := s.MediaSources(context.Background(), "AAAAAAAAAAA")
	if err != nil {
		t.Fatalf("want the other upload to play: %v", err)
	}
	if srcs[0].URL != "https://x/b" {
		t.Fatalf("sources: %+v", srcs)
	}
	if q := calls[2][len(calls[2])-1]; !strings.Contains(q, "Peacock") || !strings.Contains(q, "audio") {
		t.Fatalf("fallback search should use the song's title: %q", q)
	}
}
