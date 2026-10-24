package deepsearch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/observability"
)

// countingRunner counts runs and can block until released.
type countingRunner struct {
	runs    atomic.Int32
	release chan struct{}
	out     []byte
	err     error
}

func (r *countingRunner) run(ctx context.Context, args []string, max int64) ([]byte, error) {
	r.runs.Add(1)
	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.out, r.err
}

func TestSearchCacheHitMissAndExpiry(t *testing.T) {
	r := &countingRunner{out: searchJSON()}
	c := cache.New(100)
	now := time.Unix(1000, 0)
	c.SetClock(func() time.Time { return now })
	m := observability.NewMetrics()
	s := New(Options{Runner: r.run, SearchTTL: time.Minute, Cache: c, Metrics: m})

	for _, q := range []string{"Song A", "  song   a ", "SONG A"} {
		res, err := s.Search(context.Background(), q, 3)
		if err != nil || len(res) != 2 {
			t.Fatalf("%q: %v %v", q, res, err)
		}
	}
	if r.runs.Load() != 1 {
		t.Fatalf("case and spacing should share one cache entry: runs=%d", r.runs.Load())
	}
	if _, err := s.Search(context.Background(), "song a", 2); err != nil || r.runs.Load() != 2 {
		t.Fatalf("a different limit is a different key: runs=%d", r.runs.Load())
	}
	now = now.Add(61 * time.Second)
	if _, err := s.Search(context.Background(), "song a", 3); err != nil || r.runs.Load() != 3 {
		t.Fatalf("expired results must be fetched again: runs=%d", r.runs.Load())
	}
	if m.Counter(`deepsearch_cache_total{result="hit"}`).Load() != 2 {
		t.Errorf("hits = %d", m.Counter(`deepsearch_cache_total{result="hit"}`).Load())
	}
	if m.HistogramCount("deepsearch_search") != 5 || m.HistogramCount("ytdlp_run") != 3 {
		t.Errorf("timings: search=%d run=%d", m.HistogramCount("deepsearch_search"), m.HistogramCount("ytdlp_run"))
	}
}

func TestCachedResultsAreCopies(t *testing.T) {
	r := &countingRunner{out: searchJSON()}
	s := New(Options{Runner: r.run, SearchTTL: time.Minute})
	a, _ := s.Search(context.Background(), "song a", 3)
	a[0].Title = "changed by a caller"
	b, _ := s.Search(context.Background(), "song a", 3)
	if b[0].Title == "changed by a caller" {
		t.Fatal("callers must not share the cached slice")
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	r := &countingRunner{err: &exitError{err: errors.New("exit 1"), stderr: "ERROR: something broke"}}
	s := New(Options{Runner: r.run, SearchTTL: time.Minute})
	if _, err := s.Search(context.Background(), "song a", 3); err == nil {
		t.Fatal("want error")
	}
	r.err, r.out = nil, searchJSON()
	if res, err := s.Search(context.Background(), "song a", 3); err != nil || len(res) != 2 {
		t.Fatalf("a failure must not be served from the cache: %v %v", res, err)
	}
}

func TestConcurrentIdenticalSearchesShareOneProcess(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Minute} {
		r := &countingRunner{out: searchJSON(), release: make(chan struct{})}
		s := New(Options{Runner: r.run, SearchTTL: ttl, Concurrency: 4})
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if res, err := s.Search(context.Background(), "same query", 3); err != nil || len(res) != 2 {
					t.Errorf("ttl %v: %v %v", ttl, res, err)
				}
			}()
		}
		for r.runs.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
		close(r.release)
		wg.Wait()
		if r.runs.Load() != 1 {
			t.Fatalf("ttl %v: want one yt-dlp run, got %d", ttl, r.runs.Load())
		}
	}
}

func TestConcurrentResolvesShareOneProcess(t *testing.T) {
	r := &countingRunner{release: make(chan struct{}), out: []byte(`{"id":"dQw4w9WgXcQ","availability":"public","formats":[
		{"format_id":"a","url":"https://x/a","ext":"m4a","acodec":"mp4a","vcodec":"none","abr":128,"protocol":"https"}]}`)}
	s := New(Options{Runner: r.run, Playback: true, Concurrency: 4})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if srcs, _, err := s.MediaSources(context.Background(), "dQw4w9WgXcQ"); err != nil || len(srcs) != 1 {
				t.Errorf("%v %v", srcs, err)
			}
		}()
	}
	for r.runs.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	close(r.release)
	wg.Wait()
	if r.runs.Load() != 1 {
		t.Fatalf("want one resolve run, got %d", r.runs.Load())
	}
}

func TestAbandonedSearchStopsTheProcess(t *testing.T) {
	stopped := make(chan struct{})
	run := func(ctx context.Context, args []string, max int64) ([]byte, error) {
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}
	s := New(Options{Runner: run, Timeout: time.Minute, SearchTTL: time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Search(ctx, "song", 3); catalog.KindOf(err) != catalog.KindRateLimited {
		t.Fatalf("the caller that left gets a busy error, got %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("yt-dlp kept running for nobody")
	}
}

func TestBackgroundWorkNeverWaitsAndLeavesASlot(t *testing.T) {
	release := make(chan struct{})
	var runs atomic.Int32
	run := func(ctx context.Context, args []string, max int64) ([]byte, error) {
		runs.Add(1)
		<-release
		return searchJSON(), nil
	}
	m := observability.NewMetrics()
	s := New(Options{Runner: run, Concurrency: 2, Metrics: m})
	bg := catalog.Background(context.Background())

	// One listener search occupies a slot.
	go s.Search(context.Background(), "listener one", 3)
	for runs.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	// With one slot left, background work is refused at once so a second
	// listener still finds a free slot.
	start := time.Now()
	_, err := s.Search(bg, "prefetch", 3)
	if !catalog.IsBackgroundBusy(err) || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("background should be refused immediately, got %v after %v", err, time.Since(start))
	}
	go s.Search(context.Background(), "listener two", 3)
	for runs.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	if m.Counter("deepsearch_background_refused_total").Load() != 1 {
		t.Fatal("refusal should be counted")
	}

	// With every slot free, background work runs.
	s2 := New(Options{Runner: (&countingRunner{out: searchJSON()}).run, Concurrency: 2})
	if _, err := s2.Search(bg, "prefetch", 3); err != nil {
		t.Fatalf("idle server should run background work: %v", err)
	}
}

func TestCloseStopsRunsAndRefusesNewOnes(t *testing.T) {
	started := make(chan struct{})
	run := func(ctx context.Context, args []string, max int64) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s := New(Options{Runner: run, Timeout: time.Minute})
	errc := make(chan error, 1)
	go func() {
		_, err := s.Search(context.Background(), "song", 3)
		errc <- err
	}()
	<-started
	begin := time.Now()
	s.Close(time.Second)
	if time.Since(begin) > 500*time.Millisecond {
		t.Fatal("Close should not wait for the run's timeout")
	}
	if err := <-errc; catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("in-flight search should report shutdown, got %v", err)
	}
	if _, err := s.Search(context.Background(), "other", 3); catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("new work must be refused after Close, got %v", err)
	}
	s.Close(time.Second) // idempotent
}

func TestStatusReportsVersionWithoutDetails(t *testing.T) {
	r := &countingRunner{out: []byte("2026.10.01\n<script>\n")}
	s := New(Options{Runner: r.run, Playback: true, Concurrency: 3})
	st := s.Status(context.Background())
	if !st.Available || st.Version != "2026.10.01" || st.Capacity != 3 || !st.Playback {
		t.Fatalf("status: %+v", st)
	}
	s.Status(context.Background())
	if r.runs.Load() != 1 {
		t.Fatalf("the version should be remembered, runs=%d", r.runs.Load())
	}

	missing := New(Options{Binary: "definitely-not-yt-dlp-binary"})
	st = missing.Status(context.Background())
	if st.Available || st.Reason == "" || st.Version != "" {
		t.Fatalf("missing binary: %+v", st)
	}
}

func TestCleanVersion(t *testing.T) {
	for in, want := range map[string]string{
		"2026.10.01\n":                  "2026.10.01",
		"  2026.10.01.123456+nightly  ": "2026.10.01.123456+nightly",
		"/usr/bin/x; rm -rf":            "usrbinxrm-rf",
	} {
		if got := cleanVersion(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
