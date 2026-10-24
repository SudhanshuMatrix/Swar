package playback

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/observability"
	"swar/internal/testkit"
)

func TestDescriptorUnavailableWhenDisabled(t *testing.T) {
	cat, f := testkit.Catalog(t, testkit.Options{})
	r := NewResolver(cat, cache.New(100), nil)
	d, err := r.Resolve(context.Background(), "saavn:s1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Available || d.Reason == "" || len(d.Sources) != 0 {
		t.Fatalf("want unavailable descriptor: %+v", d)
	}
	if f.Calls.Load() != 0 {
		t.Error("no upstream call expected when media is disabled")
	}
}

func TestDescriptorCachedUntilRefreshPoint(t *testing.T) {
	cat, f := testkit.Catalog(t, testkit.Options{MediaURLs: true})
	c := cache.New(100)
	r := NewResolver(cat, c, nil)
	d, err := r.Resolve(context.Background(), "saavn:s1")
	if err != nil || !d.Available || len(d.Sources) != 5 || d.ExpiresAt == nil || d.RefreshAfter == nil {
		t.Fatalf("descriptor: %+v %v", d, err)
	}
	if !d.RefreshAfter.Before(*d.ExpiresAt) {
		t.Error("refresh must come before expiry")
	}
	calls := f.Count("song.getDetails")
	_, _ = r.Resolve(context.Background(), "s1") // bare ID, same track
	if f.Count("song.getDetails") != calls {
		t.Error("second resolve should be cached")
	}
	// Past the refresh point the cached descriptor must not be served.
	future := time.Now().Add(4 * time.Hour)
	r.now = func() time.Time { return future }
	c.SetClock(func() time.Time { return future })
	_, _ = r.Resolve(context.Background(), "saavn:s1")
	if f.Count("song.getDetails") <= calls {
		t.Error("expired descriptor must be re-resolved")
	}
	r.Invalidate("saavn:s1")
	if _, ok := c.Get("desc:saavn:s1"); ok {
		t.Error("invalidate should drop the cached descriptor")
	}
}

func TestPrefetchLimitsAndDataSaver(t *testing.T) {
	cat, _ := testkit.Catalog(t, testkit.Options{MediaURLs: true})
	r := NewResolver(cat, cache.New(100), nil)
	res, err := r.Prefetch(context.Background(), "dev", []string{"saavn:a", "saavn:a", "saavn:b", "saavn:c"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].TrackID != "saavn:a" || res[1].TrackID != "saavn:b" {
		t.Fatalf("want next two unique items: %+v", res)
	}
	if res[0].Descriptor == nil || !res[0].Descriptor.Available || res[0].Track == nil {
		t.Fatalf("prefetch should include metadata and descriptor: %+v", res[0])
	}
	res, _ = r.Prefetch(context.Background(), "dev", []string{"saavn:a", "saavn:b"}, true)
	if len(res) != 1 {
		t.Fatal("data saver prefetches only the next item")
	}
}

func TestNewPrefetchCancelsOld(t *testing.T) {
	cat, f := testkit.Catalog(t, testkit.Options{MediaURLs: true})
	f.Delay("song.getDetails", 300*time.Millisecond)
	r := NewResolver(cat, cache.New(100), nil)
	errCh := make(chan error, 1)
	go func() {
		_, err := r.Prefetch(context.Background(), "dev", []string{"saavn:old"}, false)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := r.Prefetch(context.Background(), "dev", []string{"saavn:new"}, true); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err == nil {
		t.Fatal("superseded prefetch should report cancellation")
	}
}

// busySource refuses background work (like Deep Search when listeners need
// its slots) and serves listeners.
type busySource struct {
	calls   atomic.Int32
	release chan struct{}
}

func (b *busySource) Track(ctx context.Context, id string) (*catalog.Track, error) {
	return &catalog.Track{ID: "yt:" + id, Provider: catalog.ProviderYouTube, ProviderID: id, Title: "T"}, nil
}

func (b *busySource) MediaSources(ctx context.Context, id string) ([]catalog.MediaSource, time.Time, error) {
	b.calls.Add(1)
	if catalog.IsBackground(ctx) {
		<-b.release
		return nil, time.Time{}, catalog.NewError(catalog.KindRateLimited, "busy", catalog.ErrBackgroundBusy)
	}
	return []catalog.MediaSource{{Kind: "audio", URL: "https://x/a"}}, time.Now().Add(time.Hour), nil
}

func TestListenerJoiningRefusedPrefetchRetries(t *testing.T) {
	// No primary catalog: "yt:" IDs go to the registered source.
	cat := catalog.NewService(nil, cache.New(100), catalog.DefaultTTLs)
	src := &busySource{release: make(chan struct{})}
	cat.RegisterSource(catalog.ProviderYouTube, src)
	m := observability.NewMetrics()
	r := NewResolver(cat, cache.New(100), m)

	pre := make(chan []PrefetchResult, 1)
	go func() {
		res, _ := r.Prefetch(context.Background(), "dev", []string{"yt:dQw4w9WgXcQ"}, true)
		pre <- res
	}()
	waitFor(t, func() bool { return src.calls.Load() > 0 })
	got := make(chan *Descriptor, 1)
	go func() {
		d, err := r.Resolve(context.Background(), "yt:dQw4w9WgXcQ")
		if err != nil {
			t.Errorf("listener resolve: %v", err)
		}
		got <- d
	}()
	waitFor(t, func() bool { return r.flights.Coalesced() > 0 })
	close(src.release)
	if d := <-got; d == nil || !d.Available {
		t.Fatalf("listener should get a playable descriptor: %+v", d)
	}
	res := <-pre
	if len(res) != 1 || res[0].Error == "" {
		t.Fatalf("the refused prefetch reports an error: %+v", res)
	}
	if m.Counter("playback_background_retry_total").Load() != 1 || m.Counter(`prefetch_items_total{result="error"}`).Load() != 1 {
		t.Fatal("retry and prefetch result should be counted")
	}
	if m.HistogramCount("playback_resolve") < 2 {
		t.Fatal("resolve latency should be recorded")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
