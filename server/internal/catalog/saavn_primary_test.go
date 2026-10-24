package catalog_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/observability"
	"swar/internal/playback"
	"swar/internal/providers/saavn"
	"swar/internal/providers/saavn/saavntest"
)

// The real Saavn client as the primary, the way cmd/server wires it. The
// testkit fake accepts "saavn:" IDs, so only this setup shows what Saavn
// receives.
func saavnCatalog(t *testing.T) (*catalog.Service, *saavntest.Fake) {
	t.Helper()
	f := saavntest.New()
	t.Cleanup(f.Close)
	c, err := saavn.New(saavn.Options{BaseURL: f.APIURL(), Timeout: 2 * time.Second, MaxConcurrency: 4, MaxBodyBytes: 1 << 20, MediaURLs: true})
	if err != nil {
		t.Fatal(err)
	}
	return catalog.NewService(c, cache.New(100), catalog.DefaultTTLs), f
}

// Regression: the catalog sent "saavn:s1" to Saavn, which knows the song
// as "s1", so every song resolved to "track not found" and nothing played.
func TestSaavnSongsPlayThroughCatalog(t *testing.T) {
	cat, _ := saavnCatalog(t)
	ctx := context.Background()

	tr, err := cat.Track(ctx, "saavn:s1")
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if tr.ID != "saavn:s1" {
		t.Fatalf("track id = %q", tr.ID)
	}
	ts, err := cat.Tracks(ctx, []string{"saavn:s7", "s8"})
	if err != nil || len(ts) != 2 {
		t.Fatalf("tracks: %v %+v", err, ts)
	}

	srcs, _, err := cat.MediaSources(ctx, "saavn:s1")
	if err != nil || len(srcs) == 0 {
		t.Fatalf("media sources: %v %+v", err, srcs)
	}

	d, err := playback.NewResolver(cat, cache.New(100), observability.NewMetrics()).Resolve(ctx, "saavn:s2")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Available || len(d.Sources) == 0 {
		t.Fatalf("descriptor not playable: %+v", d)
	}
	if !strings.HasPrefix(d.Sources[0].URL, "https://aac.saavncdn.com/s2_") {
		t.Fatalf("source url = %q", d.Sources[0].URL)
	}
}
