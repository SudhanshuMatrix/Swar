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
//
// Performance rules:
//   - Identical concurrent searches and resolves share one yt-dlp process,
//     and that process is stopped when every caller waiting for it leaves.
//   - Search results are cached per normalized query and limit. Errors,
//     timeouts and refusals are never cached.
//   - Background work (prefetching) never waits for a slot and leaves one
//     slot free for searches and playback a listener is waiting for.
//   - Close stops every running process (its whole process group where the
//     platform has one) and refuses new work.
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
	"sync"
	"sync/atomic"
	"time"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/observability"
)

// Runner executes yt-dlp with args and returns stdout, never more than
// maxOut bytes. Swappable for tests.
type Runner func(ctx context.Context, args []string, maxOut int64) ([]byte, error)

type Options struct {
	Binary string
	// BinaryPath, when set, is asked for the executable before each run
	// (a managed, verified install that may change after an update). It
	// wins over Binary when it returns a non-empty path.
	BinaryPath  func() string
	Timeout     time.Duration
	MaxResults  int
	Concurrency int
	MaxOutput   int64
	Playback    bool // allow resolving candidates to stream URLs
	Downloads   bool // operator asserts offline copies are permitted
	Runner      Runner
	Metrics     *observability.Metrics

	// SearchTTL is how long search results are reused; 0 disables the
	// result cache (identical concurrent searches are still shared).
	SearchTTL time.Duration
	// Cache holds search results. When nil and SearchTTL > 0, the service
	// keeps its own cache of CacheEntries queries.
	Cache        *cache.Cache
	CacheEntries int
}

type Service struct {
	opt Options
	sem chan struct{}
	run Runner
	m   *observability.Metrics

	cache   *cache.Cache
	flights cache.Group

	// Background work takes a slot only under bgMu, so two prefetches
	// cannot both take the slot kept free for listeners.
	bgMu    sync.Mutex
	waiting atomic.Int32 // foreground callers waiting for a slot

	// Lifetime: Close cancels base, which stops every run.
	base    context.Context
	stop    context.CancelFunc
	closed  atomic.Bool
	running sync.WaitGroup
	active  atomic.Int32

	versionMu  sync.Mutex
	version    string
	versionErr error
	versionAt  time.Time

	// Title and artist of recently seen videos, so a video that cannot be
	// played can be swapped for another public upload of the same song.
	metaMu sync.Mutex
	meta   map[string]string
}

const maxMeta = 4000

func (s *Service) rememberMeta(id, title, artist string) {
	q := strings.TrimSpace(title + " " + artist)
	if id == "" || q == "" {
		return
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	if s.meta == nil || len(s.meta) >= maxMeta {
		s.meta = map[string]string{}
	}
	s.meta[id] = q
}

// ClearCache drops every cached search result and returns how many there
// were. Searches already running are shared as before. The title/artist
// notes kept for swapping an unplayable video stay: they are not results.
func (s *Service) ClearCache() int {
	if s.cache == nil {
		return 0
	}
	return s.cache.Clear()
}

func (s *Service) metaQuery(id string) string {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	return s.meta[id]
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
	if o.CacheEntries < 1 {
		o.CacheEntries = 500
	}
	s := &Service{opt: o, sem: make(chan struct{}, o.Concurrency), run: o.Runner, m: o.Metrics}
	s.base, s.stop = context.WithCancel(context.Background())
	if s.run == nil {
		s.run = execRunner(s.binary, o.Metrics)
	}
	if o.SearchTTL > 0 {
		s.cache = o.Cache
		if s.cache == nil {
			s.cache = cache.New(o.CacheEntries)
		}
	}
	return s
}

// binary is the executable to run: the managed install when there is one,
// otherwise the configured name or path.
func (s *Service) binary() string {
	if s.opt.BinaryPath != nil {
		if p := s.opt.BinaryPath(); p != "" {
			return p
		}
	}
	if s.opt.Binary == "" {
		return "yt-dlp"
	}
	return s.opt.Binary
}

// Close stops every running yt-dlp process, refuses new work and waits up
// to wait for the processes to exit. It is safe to call more than once.
func (s *Service) Close(wait time.Duration) {
	s.closed.Store(true)
	s.stop()
	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// exec runs yt-dlp once: tied to the service's lifetime, counted, timed.
func (s *Service) exec(ctx context.Context, args []string, maxOut int64) ([]byte, error) {
	if s.closed.Load() {
		return nil, catalog.Unavailable("Deep Search is shutting down")
	}
	s.running.Add(1)
	defer s.running.Done()
	s.active.Add(1)
	defer s.active.Add(-1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.base, cancel)
	defer stop()
	start := time.Now()
	out, err := s.run(ctx, args, maxOut)
	s.m.Observe("ytdlp_run", time.Since(start))
	if err != nil && s.closed.Load() {
		return nil, catalog.NewError(catalog.KindUnavailable, "Deep Search is shutting down", err)
	}
	return out, err
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

// acquire takes a yt-dlp slot. Listeners wait for one (until their ctx
// ends). Background work never waits: it only starts when a slot is free,
// no listener is waiting, and (with more than one slot) one slot stays free.
func (s *Service) acquire(ctx context.Context) (func(), error) {
	release := func() { <-s.sem }
	if catalog.IsBackground(ctx) {
		s.bgMu.Lock()
		defer s.bgMu.Unlock()
		reserve := 0
		if cap(s.sem) > 1 {
			reserve = 1
		}
		if s.waiting.Load() == 0 && len(s.sem) < cap(s.sem)-reserve {
			select {
			case s.sem <- struct{}{}:
				return release, nil
			default:
			}
		}
		s.m.Inc("deepsearch_background_refused_total")
		return nil, catalog.NewError(catalog.KindRateLimited, "Deep Search is busy with listeners' requests", catalog.ErrBackgroundBusy)
	}
	s.waiting.Add(1)
	defer s.waiting.Add(-1)
	select {
	case s.sem <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, busy(ctx.Err())
	}
}

func busy(cause error) error {
	return catalog.NewError(catalog.KindRateLimited, "Deep Search is busy; try again in a moment", cause)
}

// shared runs fn once for every concurrent caller of key. A caller whose
// ctx ends while waiting gets a "busy" error; the work stops when nobody
// is left waiting for it.
func shared[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := cache.Do(ctx, &s.flights, key, s.sharedTimeout(), fn)
	return v, s.callerErr(ctx, err)
}

// sharedTimeout bounds shared work: time to wait for a slot plus a run
// (and, for resolves, the fallback search and a few alternative runs).
func (s *Service) sharedTimeout() time.Duration { return 4*s.opt.Timeout + 5*time.Second }

func (s *Service) callerErr(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return busy(err)
	}
	return err
}

// SearchKey is the cache key for a search: the provider namespace, the
// normalized target (case and whitespace folded) and the limit.
func SearchKey(target string, limit int) string {
	return fmt.Sprintf("deepsearch:yt:search:%s|%d", strings.ToLower(strings.Join(strings.Fields(target), " ")), limit)
}

// Search returns up to limit metadata candidates. It never downloads media.
// Results are cached (see Options.SearchTTL) and identical concurrent
// searches share one yt-dlp run.
func (s *Service) Search(ctx context.Context, input string, limit int) ([]Candidate, error) {
	if limit < 1 || limit > s.opt.MaxResults {
		limit = s.opt.MaxResults
	}
	query, videoID, err := ParseInput(input)
	if err != nil {
		return nil, err
	}
	target := watchURL(videoID)
	if videoID == "" {
		target = fmt.Sprintf("ytsearch%d:%s", limit, query)
	}
	start := time.Now()
	defer s.m.Since("deepsearch_search", start)
	key := SearchKey(target, limit)
	if s.cache == nil {
		return shared(ctx, s, key, func(ctx context.Context) ([]Candidate, error) {
			return s.searchRun(ctx, target, limit)
		})
	}
	if _, hit := s.cache.Peek(key); hit {
		s.m.Inc(`deepsearch_cache_total{result="hit"}`)
	} else {
		s.m.Inc(`deepsearch_cache_total{result="miss"}`)
	}
	res, err := cache.GetOrLoadShared(ctx, s.cache, &s.flights, key, s.sharedTimeout(), func(ctx context.Context) ([]Candidate, time.Duration, error) {
		res, err := s.searchRun(ctx, target, limit)
		return res, s.opt.SearchTTL, err
	})
	if err != nil {
		return nil, s.callerErr(ctx, err)
	}
	// Results are shared between callers: hand each one its own slice.
	return append([]Candidate(nil), res...), nil
}

func (s *Service) searchRun(ctx context.Context, target string, limit int) ([]Candidate, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, s.opt.Timeout)
	defer cancel()
	s.m.Inc("deepsearch_runs_total")
	args := []string{
		"--ignore-config", "--no-warnings", "--skip-download", "--flat-playlist",
		"--dump-single-json", "--no-playlist", "--socket-timeout", "10",
		"--", target,
	}
	out, err := s.exec(ctx, args, s.opt.MaxOutput)
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
	s.rememberMeta(e.ID, title, strings.TrimSuffix(artist, "VEVO"))
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
	srcs, until, err := s.resolveVideo(ctx, id)
	if err == nil || !replaceable(err) {
		return srcs, until, err
	}
	// Age-gated, members-only or label-blocked uploads are common for
	// international releases. Another public upload of the same song
	// (official audio, lyric video, topic track) usually plays.
	q := s.metaQuery(id)
	if q == "" {
		return nil, time.Time{}, err
	}
	cands, serr := s.Search(ctx, q+" audio", 5)
	if serr != nil {
		return nil, time.Time{}, err
	}
	tried := 0
	for _, c := range cands {
		if c.ProviderID == id || tried >= 3 || ctx.Err() != nil {
			continue
		}
		tried++
		if alt, altUntil, aerr := s.resolveVideo(ctx, c.ProviderID); aerr == nil {
			s.m.Inc("deepsearch_resolve_fallbacks_total")
			return alt, altUntil, nil
		}
	}
	return nil, time.Time{}, err
}

// replaceable reports whether a failed resolve may be retried with another
// upload of the same song (not timeouts, rate limits or a missing binary).
func replaceable(err error) bool {
	if errors.Is(err, errNotInstalled) {
		return false
	}
	switch catalog.KindOf(err) {
	case catalog.KindUnavailable, catalog.KindUpstream:
		return true
	}
	return false
}

type resolved struct {
	sources []catalog.MediaSource
	until   time.Time
}

// resolveVideo extracts stream references for one video. Concurrent
// resolves of the same video (player, relay, prefetch, download check)
// share one run. Results are not cached here: the playback package caches
// descriptors and knows when they expire.
func (s *Service) resolveVideo(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	start := time.Now()
	defer s.m.Since("deepsearch_resolve", start)
	r, err := shared(ctx, s, "deepsearch:yt:resolve:"+id, func(ctx context.Context) (resolved, error) {
		srcs, until, err := s.resolveRun(ctx, id)
		return resolved{srcs, until}, err
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	return append([]catalog.MediaSource(nil), r.sources...), r.until, nil
}

func (s *Service) resolveRun(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
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
		"--no-playlist", "--socket-timeout", "10",
		// Popular videos list captions in every language, which can make
		// the JSON larger than the output limit.
		"--extractor-args", "youtube:skip=translated_subs",
		"--", watchURL(id),
	}
	limit := s.opt.MaxOutput
	if limit < 32<<20 {
		limit = 32 << 20
	}
	out, err := s.exec(ctx, args, limit)
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
	if ce := (*catalog.Error)(nil); errors.As(err, &ce) {
		return err // already classified (shutdown)
	}
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
	if errors.As(err, &ee) && strings.Contains(ee.stderr, "not a bot") {
		// YouTube is refusing this server's address, not this song, so
		// trying other uploads would only fail the same way.
		s.m.Inc(`deepsearch_errors_total{kind="blocked"}`)
		return catalog.NewError(catalog.KindRateLimited, "YouTube is blocking requests from this server right now", err)
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

// execRunner starts the binary directly (no shell) with an argument vector,
// so spaces, quotes and Unicode in paths or queries are passed as is. The
// process runs in its own process group where the platform has them, so a
// cancelled run also stops helpers yt-dlp started.
func execRunner(binary func() string, m *observability.Metrics) Runner {
	return func(ctx context.Context, args []string, maxOut int64) ([]byte, error) {
		path, err := exec.LookPath(binary())
		if err != nil {
			return nil, errNotInstalled
		}
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.WaitDelay = 2 * time.Second
		killProcessGroup(cmd)
		stdout := &limitedBuffer{max: maxOut}
		stderr := &limitedBuffer{max: 64 << 10}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Stdin = nil
		started := time.Now()
		if err = cmd.Start(); err != nil {
			return nil, &exitError{err: err, stderr: "could not start yt-dlp"}
		}
		m.Observe("ytdlp_start", time.Since(started))
		err = cmd.Wait()
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
