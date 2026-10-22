// Package saavn integrates the unofficial JioSaavn web API (api.php) as a
// catalog provider. The API is undocumented and may change or disappear at
// any time; every response is decoded tolerantly and every failure is
// mapped to a catalog.Error.
package saavn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"swar/internal/catalog"
	"swar/internal/observability"
)

// API "__call" names, taken from the reference implementation.
const (
	callSearchSongs     = "search.getResults"
	callSearchAlbums    = "search.getAlbumResults"
	callSearchArtists   = "search.getArtistResults"
	callSearchPlaylists = "search.getPlaylistResults"
	callSongDetails     = "song.getDetails"
	callWebAPI          = "webapi.get"
	callStationSongs    = "webradio.getSong"
	callCreateStation   = "webradio.createEntityStation"
	callLyrics          = "lyrics.getLyrics"
	callAlbum           = "content.getAlbumDetails"
	callArtist          = "artist.getArtistPageDetails"
	callArtistSongs     = "artist.getArtistMoreSong"
	callArtistAlbums    = "artist.getArtistMoreAlbum"
	callPlaylist        = "playlist.getDetails"
	callBrowseModules   = "content.getBrowseModules"
	callTrending        = "content.getTrending"
)

const (
	ctxWeb     = "web6dot0"
	ctxAndroid = "android"
)

type Options struct {
	BaseURL        string
	Timeout        time.Duration
	MaxConcurrency int
	MaxRetries     int
	MaxBodyBytes   int64
	MediaURLs      bool
	Downloads      bool
	HTTPClient     *http.Client
	Logger         *slog.Logger
	Metrics        *observability.Metrics
}

type Client struct {
	base       *url.URL
	http       *http.Client
	sem        chan struct{}
	maxRetries int
	maxBody    int64
	timeout    time.Duration
	mediaURLs  bool
	downloads  bool
	log        *slog.Logger
	metrics    *observability.Metrics
	// sleep is replaceable in tests so backoff does not slow them down.
	sleep func(ctx context.Context, d time.Duration) error
}

func New(o Options) (*Client, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid Saavn base URL %q", o.BaseURL)
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 5 * time.Second,
			},
			// Never follow redirects to arbitrary hosts.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if o.MaxConcurrency < 1 {
		o.MaxConcurrency = 1
	}
	if o.Timeout <= 0 {
		o.Timeout = 8 * time.Second
	}
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = 4 << 20
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Client{
		base: u, http: hc, sem: make(chan struct{}, o.MaxConcurrency),
		maxRetries: o.MaxRetries, maxBody: o.MaxBodyBytes, timeout: o.Timeout,
		mediaURLs: o.MediaURLs, downloads: o.Downloads, log: o.Logger, metrics: o.Metrics,
		sleep: sleepCtx,
	}, nil
}

func (c *Client) Name() string { return catalog.ProviderSaavn }

// get calls one api.php endpoint and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, call string, params url.Values, apiCtx string, out any) error {
	q := url.Values{}
	q.Set("__call", call)
	q.Set("_format", "json")
	q.Set("_marker", "0")
	q.Set("api_version", "4")
	q.Set("ctx", apiCtx)
	for k, vs := range params {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u := *c.base
	u.RawQuery = q.Encode()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, backoff(attempt, lastErr)); err != nil {
				return timeoutOr(err)
			}
		}
		body, err := c.do(ctx, u.String())
		if err == nil {
			if len(body) == 0 || string(body) == "null" {
				return catalog.NotFound("not found")
			}
			if err := json.Unmarshal(body, out); err != nil {
				c.metrics.Inc(`saavn_errors_total{kind="decode"}`)
				return catalog.NewError(catalog.KindUpstream, "the music catalog returned an unexpected response", err)
			}
			return nil
		}
		lastErr = err
		if !retryable(err) || ctx.Err() != nil {
			break
		}
		c.metrics.Inc("saavn_retries_total")
	}
	return lastErr
}

type statusError struct {
	code       int
	retryAfter time.Duration
}

func (e *statusError) Error() string { return "upstream status " + strconv.Itoa(e.code) }

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, error) {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, timeoutOr(ctx.Err())
	}
	rctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, catalog.NewError(catalog.KindUpstream, "could not build upstream request", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgents[rand.IntN(len(userAgents))])
	start := time.Now()
	resp, err := c.http.Do(req)
	c.metrics.Inc("saavn_requests_total")
	if err != nil {
		c.metrics.Inc(`saavn_errors_total{kind="network"}`)
		if errors.Is(err, context.DeadlineExceeded) || rctx.Err() != nil {
			return nil, catalog.NewError(catalog.KindTimeout, "the music catalog took too long to respond", err)
		}
		return nil, catalog.NewError(catalog.KindUpstream, "the music catalog is unreachable", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return nil, catalog.NewError(catalog.KindUpstream, "the music catalog response was interrupted", err)
	}
	if int64(len(body)) > c.maxBody {
		c.metrics.Inc(`saavn_errors_total{kind="too_large"}`)
		return nil, catalog.NewError(catalog.KindUpstream, "the music catalog response was too large", nil)
	}
	c.log.Debug("saavn request", "status", resp.StatusCode, "ms", time.Since(start).Milliseconds())
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		c.metrics.Inc(`saavn_errors_total{kind="rate_limited"}`)
		return nil, catalog.NewError(catalog.KindRateLimited, "the music catalog is rate limiting requests; try again shortly",
			&statusError{code: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))})
	case resp.StatusCode >= 500:
		c.metrics.Inc(`saavn_errors_total{kind="5xx"}`)
		return nil, catalog.NewError(catalog.KindUpstream, "the music catalog is having problems", &statusError{code: resp.StatusCode})
	case resp.StatusCode == http.StatusNotFound:
		return nil, catalog.NotFound("not found")
	case resp.StatusCode >= 400:
		c.metrics.Inc(`saavn_errors_total{kind="4xx"}`)
		return nil, catalog.NewError(catalog.KindUpstream, "the music catalog rejected the request", &statusError{code: resp.StatusCode})
	}
	return body, nil
}

func retryable(err error) bool {
	switch catalog.KindOf(err) {
	case catalog.KindTimeout, catalog.KindRateLimited:
		return true
	case catalog.KindUpstream:
		var se *statusError
		if errors.As(err, &se) {
			return se.code >= 500
		}
		// Network errors (no status) are transient; decode errors are not
		// produced by do().
		return true
	}
	return false
}

// backoff is exponential with full jitter, honouring a bounded Retry-After.
func backoff(attempt int, lastErr error) time.Duration {
	var se *statusError
	if errors.As(lastErr, &se) && se.retryAfter > 0 {
		return min(se.retryAfter, 5*time.Second)
	}
	base := 200 * time.Millisecond << (attempt - 1)
	base = min(base, 3*time.Second)
	return time.Duration(rand.Int64N(int64(base))) + base/2
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func timeoutOr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return catalog.NewError(catalog.KindTimeout, "the request timed out", err)
	}
	return err
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36 Edg/134.0.0.0",
	"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Mobile Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64; rv:136.0) Gecko/20100101 Firefox/136.0",
}
