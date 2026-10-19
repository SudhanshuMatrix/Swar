// Package deepsearch implements the user-triggered "Deep Search" using
// yt-dlp for metadata and candidate discovery.
//
// Safety rules enforced here:
//   - yt-dlp is executed directly with an argument vector (never a shell),
//     with --ignore-config so no local config can inject options, and with
//     "--" before the single positional input.
//   - The only inputs are a "ytsearchN:<query>" expression or a canonical
//     YouTube watch URL rebuilt from a validated 11-character video ID.
//     Arbitrary user URLs are rejected, so this cannot become an SSRF.
//   - Every run is bounded by a timeout, an output-size limit and a
//     process-wide concurrency limit. Search runs with --flat-playlist and
//     --skip-download: no media is downloaded.
//   - Resolving a candidate into a playable stream is a separate operation
//     that is disabled unless the operator enables it.
package deepsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"swar/internal/catalog"
	"swar/internal/observability"
)

// Runner executes yt-dlp with args and returns stdout, never more than
// maxOut bytes. Swappable for tests.
type Runner func(ctx context.Context, args []string, maxOut int64) ([]byte, error)

type Options struct {
	Binary      string
	Timeout     time.Duration
	MaxResults  int
	Concurrency int
	MaxOutput   int64
	Playback    bool // allow resolving candidates to stream URLs
	Downloads   bool // operator asserts offline copies are permitted
	Runner      Runner
	Metrics     *observability.Metrics
}

type Service struct {
	opt Options
	sem chan struct{}
	run Runner
	m   *observability.Metrics
}

func New(o Options) *Service {
	if o.Concurrency < 1 {
		o.Concurrency = 1
	}
	if o.MaxResults < 1 {
		o.MaxResults = 10
	}
	if o.MaxOutput <= 0 {
		o.MaxOutput = 8 << 20
	}
	if o.Timeout <= 0 {
		o.Timeout = 25 * time.Second
	}
	s := &Service{opt: o, sem: make(chan struct{}, o.Concurrency), run: o.Runner, m: o.Metrics}
	if s.run == nil {
		s.run = execRunner(o.Binary)
	}
	return s
}

// Candidate is one deep-search result. Finding a candidate says nothing
// about whether it can be played or downloaded.
type Candidate struct {
	catalog.Track
	Source     string `json:"source"`     // "youtube"
	Confidence string `json:"confidence"` // "metadata_only"
}

var (
	ytIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	ytURLRe = regexp.MustCompile(`^(?:https?://)?(?:www\.|m\.|music\.)?(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|embed/)|youtu\.be/)([A-Za-z0-9_-]{11})(?:[?&#].*)?$`)
)

// ParseInput classifies the user's input. It returns either a search query
// or a validated video ID; anything that looks like another URL is refused.
func ParseInput(in string) (query, videoID string, err error) {
	in = strings.TrimSpace(in)
	if m := ytURLRe.FindStringSubmatch(in); m != nil {
		return "", m[1], nil
	}
	if ytIDRe.MatchString(in) && strings.ContainsAny(in, "0123456789_-") && strings.ToLower(in) != in {
		// Only treat bare tokens as IDs when they look like one (mixed
		// case plus digits or symbols); plain words stay searches.
		return "", in, nil
	}
	lower := strings.ToLower(in)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "www.") {
		return "", "", catalog.Invalid("only YouTube links are supported in Deep Search")
	}
	q, err := catalog.NormalizeQuery(in)
	if err != nil {
		return "", "", err
	}
	if len([]rune(q)) < 2 {
		return "", "", catalog.Invalid("query is too short")
	}
	return q, "", nil
}

func watchURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

func (s *Service) acquire(ctx context.Context) (func(), error) {
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, nil
	case <-ctx.Done():
		return nil, catalog.NewError(catalog.KindRateLimited, "Deep Search is busy; try again in a moment", ctx.Err())
	}
}

// Search returns up to limit metadata candidates. It never downloads media.
func (s *Service) Search(ctx context.Context, input string, limit int) ([]Candidate, error) {
	if limit < 1 || limit > s.opt.MaxResults {
		limit = s.opt.MaxResults
	}
	query, videoID, err := ParseInput(input)
	if err != nil {
		return nil, err
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, s.opt.Timeout)
	defer cancel()
	s.m.Inc("deepsearch_runs_total")

	target := watchURL(videoID)
	if videoID == "" {
		target = fmt.Sprintf("ytsearch%d:%s", limit, query)
	}
	args := []string{
		"--ignore-config", "--no-warnings", "--skip-download", "--flat-playlist",
		"--dump-single-json", "--no-playlist", "--socket-timeout", "10",
		"--", target,
	}
	out, err := s.run(ctx, args, s.opt.MaxOutput)
	if err != nil {
		return nil, s.classify(ctx, err)
	}
	return s.parseSearch(out, limit)
}

type ytEntry struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Channel    string  `json:"channel"`
	Uploader   string  `json:"uploader"`
	Artist     string  `json:"artist"`
	Track      string  `json:"track"`
	Album      string  `json:"album"`
	Duration   float64 `json:"duration"`
	ReleaseYr  int     `json:"release_year"`
	Thumbnail  string  `json:"thumbnail"`
	Thumbnails []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"thumbnails"`
	LiveStatus   string            `json:"live_status"`
	Availability string            `json:"availability"`
	Entries      []json.RawMessage `json:"entries"`
	Formats      []ytFormat        `json:"formats"`
}

type ytFormat struct {
	FormatID string  `json:"format_id"`
	URL      string  `json:"url"`
	Ext      string  `json:"ext"`
	ACodec   string  `json:"acodec"`
	VCodec   string  `json:"vcodec"`
	ABR      float64 `json:"abr"`
	Height   int     `json:"height"`
	Protocol string  `json:"protocol"`
	HasDRM   any     `json:"has_drm"`
}

func (s *Service) parseSearch(out []byte, limit int) ([]Candidate, error) {
	var root ytEntry
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, catalog.NewError(catalog.KindUpstream, "Deep Search returned an unreadable response", err)
	}
	entries := []ytEntry{}
	if len(root.Entries) > 0 {
		for _, raw := range root.Entries {
			var e ytEntry
			if json.Unmarshal(raw, &e) == nil {
				entries = append(entries, e)
			}
		}
	} else if root.ID != "" {
		entries = append(entries, root)
	}
	res := make([]Candidate, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		if !ytIDRe.MatchString(e.ID) || seen[e.ID] || e.LiveStatus == "is_live" {
			continue
		}
		seen[e.ID] = true
		res = append(res, Candidate{Track: s.track(e), Source: "youtube", Confidence: "metadata_only"})
		if len(res) >= limit {
			break
		}
	}
	return res, nil
}

func (s *Service) track(e ytEntry) catalog.Track {
	artist := firstNonEmpty(e.Artist, e.Channel, e.Uploader)
	title := firstNonEmpty(e.Track, e.Title)
	t := catalog.Track{
		ID:           catalog.MakeID(catalog.ProviderYouTube, e.ID),
		Provider:     catalog.ProviderYouTube,
		ProviderID:   e.ID,
		Title:        title,
		DurationSec:  int(e.Duration),
		Images:       thumbs(e),
		Year:         e.ReleaseYr,
		URL:          watchURL(e.ID),
		Artists:      []catalog.ArtistRef{},
		Playable:     s.opt.Playback,
		ProviderMeta: map[string]string{"source": "deep_search"},
	}
	if artist != "" {
		t.Artists = append(t.Artists, catalog.ArtistRef{Name: artist, Role: "primary", Image: []catalog.Image{}})
	}
	if e.Album != "" {
		t.Album = &catalog.AlbumRef{Name: e.Album}
	}
	switch {
	case !s.opt.Playback:
		t.Download = catalog.DownloadEligibility{Reason: "source_playback_not_enabled"}
	case !s.opt.Downloads:
		t.Download = catalog.DownloadEligibility{Reason: "downloads_not_enabled_for_source"}
	default:
		t.Download = catalog.DownloadEligibility{Eligible: true, Reason: "operator_permitted"}
	}
	return t
}

func thumbs(e ytEntry) []catalog.Image {
	out := []catalog.Image{}
	for _, th := range e.Thumbnails {
		if strings.HasPrefix(th.URL, "https://") {
			q := "unknown"
			if th.Width > 0 {
				q = fmt.Sprintf("%dx%d", th.Width, th.Height)
			}
			out = append(out, catalog.Image{Quality: q, URL: th.URL})
		}
	}
	if len(out) == 0 && ytIDRe.MatchString(e.ID) {
		// Stable public thumbnail locations for a video ID.
		out = append(out,
			catalog.Image{Quality: "320x180", URL: "https://i.ytimg.com/vi/" + e.ID + "/mqdefault.jpg"},
			catalog.Image{Quality: "480x360", URL: "https://i.ytimg.com/vi/" + e.ID + "/hqdefault.jpg"})
	}
	sort.SliceStable(out, func(i, j int) bool { return area(out[i].Quality) < area(out[j].Quality) })
	if len(out) > 4 {
		out = out[len(out)-4:]
	}
	return out
}

func area(q string) int {
	w, h, ok := strings.Cut(q, "x")
	if !ok {
		return 0
	}
	a, _ := strconv.Atoi(w)
	b, _ := strconv.Atoi(h)
	return a * b
}

// Track looks up metadata for one video ID (used for queues and history).
func (s *Service) Track(ctx context.Context, id string) (*catalog.Track, error) {
	if !ytIDRe.MatchString(id) {
		return nil, catalog.Invalid("invalid video id")
	}
	c, err := s.Search(ctx, watchURL(id), 1)
	if err != nil {
		return nil, err
	}
	if len(c) == 0 {
		return nil, catalog.NotFound("video not found")
	}
	return &c[0].Track, nil
}

// MediaSources resolves stream references for a candidate. It refuses when
// playback is disabled, when the item is DRM-protected or restricted, and
// when no plain HTTPS stream exists.
func (s *Service) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	if !s.opt.Playback {
		return nil, time.Time{}, catalog.Unavailable("playback for Deep Search results is not enabled on this server")
	}
	if !ytIDRe.MatchString(id) {
		return nil, time.Time{}, catalog.Invalid("invalid video id")
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, s.opt.Timeout)
	defer cancel()
	s.m.Inc("deepsearch_resolves_total")
	args := []string{
		"--ignore-config", "--no-warnings", "--skip-download", "--dump-single-json",
		"--no-playlist", "--socket-timeout", "10", "--", watchURL(id),
	}
	out, err := s.run(ctx, args, s.opt.MaxOutput)
	if err != nil {
		return nil, time.Time{}, s.classify(ctx, err)
	}
	var e ytEntry
	if err := json.Unmarshal(out, &e); err != nil {
		return nil, time.Time{}, catalog.NewError(catalog.KindUpstream, "could not read the source's response", err)
	}
	if e.Availability != "" && e.Availability != "public" && e.Availability != "unlisted" {
		return nil, time.Time{}, catalog.Unavailable("this source is restricted and cannot be played")
	}
	return pickFormats(e.Formats)
}

func pickFormats(fs []ytFormat) ([]catalog.MediaSource, time.Time, error) {
	var bestAudio, bestVideo *ytFormat
	for i := range fs {
		f := &fs[i]
		if drm(f.HasDRM) || !strings.HasPrefix(f.URL, "https://") || !strings.HasPrefix(f.Protocol, "http") {
			continue
		}
		if f.VCodec == "none" && f.ACodec != "none" && f.ACodec != "" {
			// Prefer m4a (plays everywhere just_audio does), then bitrate.
			if bestAudio == nil || better(f, bestAudio, "m4a", func(x *ytFormat) float64 { return x.ABR }) {
				bestAudio = f
			}
		}
		if f.ACodec == "none" && f.VCodec != "none" && f.VCodec != "" && f.Height > 0 && f.Height <= 720 {
			if bestVideo == nil || better(f, bestVideo, "mp4", func(x *ytFormat) float64 { return float64(x.Height) }) {
				bestVideo = f
			}
		}
	}
	if bestAudio == nil {
		return nil, time.Time{}, catalog.Unavailable("no playable audio source is available for this item")
	}
	out := []catalog.MediaSource{{
		Kind: "audio", Quality: fmt.Sprintf("%.0fkbps", bestAudio.ABR), Bitrate: int(bestAudio.ABR),
		URL: bestAudio.URL, MimeType: mime("audio", bestAudio.Ext),
	}}
	if bestVideo != nil {
		out = append(out, catalog.MediaSource{
			Kind: "video", Quality: fmt.Sprintf("%dp", bestVideo.Height), URL: bestVideo.URL, MimeType: mime("video", bestVideo.Ext),
		})
	}
	return out, expiryOf(bestAudio.URL), nil
}

func better(a, b *ytFormat, ext string, score func(*ytFormat) float64) bool {
	if (a.Ext == ext) != (b.Ext == ext) {
		return a.Ext == ext
	}
	return score(a) > score(b)
}

func drm(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "" && x != "false"
	}
	return false
}

func mime(kind, ext string) string {
	if ext == "" {
		return ""
	}
	if ext == "m4a" {
		return "audio/mp4"
	}
	return kind + "/" + ext
}

// expiryOf reads the signed URL's "expire" parameter, defaulting to a
// short, conservative lifetime when absent.
func expiryOf(raw string) time.Time {
	const fallback = 30 * time.Minute
	u, err := url.Parse(raw)
	if err == nil {
		if n, err := strconv.ParseInt(u.Query().Get("expire"), 10, 64); err == nil {
			t := time.Unix(n, 0)
			if t.After(time.Now()) && t.Before(time.Now().Add(24*time.Hour)) {
				return t
			}
		}
	}
	return time.Now().Add(fallback)
}

var (
	errOutputTooLarge = errors.New("yt-dlp output exceeded limit")
	errNotInstalled   = errors.New("yt-dlp not installed")
)

func (s *Service) classify(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, errNotInstalled):
		return catalog.NewError(catalog.KindUnavailable, "Deep Search is not available on this server", err)
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		s.m.Inc(`deepsearch_errors_total{kind="timeout"}`)
		return catalog.NewError(catalog.KindTimeout, "Deep Search took too long; try a more specific query", err)
	case errors.Is(err, errOutputTooLarge):
		return catalog.NewError(catalog.KindUpstream, "Deep Search returned too much data", err)
	}
	s.m.Inc(`deepsearch_errors_total{kind="failed"}`)
	var ee *exitError
	if errors.As(err, &ee) && strings.Contains(strings.ToLower(ee.stderr), "drm") {
		return catalog.Unavailable("this item is DRM-protected and cannot be played")
	}
	if errors.As(err, &ee) && (strings.Contains(ee.stderr, "Private video") || strings.Contains(ee.stderr, "Sign in") || strings.Contains(ee.stderr, "unavailable")) {
		return catalog.Unavailable("this item is unavailable or restricted")
	}
	return catalog.NewError(catalog.KindUpstream, "Deep Search failed", err)
}

type exitError struct {
	err    error
	stderr string
}

func (e *exitError) Error() string { return e.err.Error() + ": " + e.stderr }
func (e *exitError) Unwrap() error { return e.err }

// limitedBuffer stops accepting writes past max and remembers it overflowed.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int64
	overflow bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if int64(l.buf.Len()+len(p)) > l.max {
		l.overflow = true
		return 0, errOutputTooLarge
	}
	return l.buf.Write(p)
}

func execRunner(binary string) Runner {
	return func(ctx context.Context, args []string, maxOut int64) ([]byte, error) {
		path, err := exec.LookPath(binary)
		if err != nil {
			return nil, errNotInstalled
		}
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.WaitDelay = 2 * time.Second
		stdout := &limitedBuffer{max: maxOut}
		stderr := &limitedBuffer{max: 64 << 10}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Stdin = nil
		err = cmd.Run()
		if stdout.overflow {
			return nil, errOutputTooLarge
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, &exitError{err: err, stderr: tail(stderr.buf.String(), 500)}
		}
		return io.ReadAll(&stdout.buf)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
