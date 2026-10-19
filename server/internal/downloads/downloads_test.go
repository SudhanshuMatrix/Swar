package downloads

import (
	"context"
	"strings"
	"testing"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/playback"
	"swar/internal/testkit"
)

func setup(t *testing.T, o testkit.Options) (*Service, string) {
	cat, _ := testkit.Catalog(t, o)
	st := testkit.Store(t)
	return New(st, cat, playback.NewResolver(cat, cache.New(100), nil)), testkit.Device(t, st, "dev_1")
}

func TestTransitionTable(t *testing.T) {
	ok := [][2]string{{"", "queued"}, {"queued", "downloading"}, {"downloading", "paused"}, {"paused", "downloading"},
		{"downloading", "verifying"}, {"verifying", "completed"}, {"completed", "failed"}, {"failed", "queued"}, {"canceled", "queued"}}
	bad := [][2]string{{"", "completed"}, {"", "downloading"}, {"queued", "completed"}, {"downloading", "completed"},
		{"paused", "completed"}, {"completed", "downloading"}, {"canceled", "completed"}}
	for _, p := range ok {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be allowed", p[0], p[1])
		}
	}
	for _, p := range bad {
		if CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be refused", p[0], p[1])
		}
	}
}

func TestIneligibleByDefault(t *testing.T) {
	s, d := setup(t, testkit.Options{})
	e, err := s.Eligibility(context.Background(), "saavn:s1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Eligible || e.Descriptor != nil || e.LocalID == "" {
		t.Fatalf("eligibility: %+v", e)
	}
	if _, err := s.Report(context.Background(), d, "saavn:s1", Update{Status: StatusQueued}); catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("queueing an ineligible track must fail: %v", err)
	}
}

func TestFullLifecycleRequiresVerifiedFile(t *testing.T) {
	s, d := setup(t, testkit.Options{MediaURLs: true, Downloads: true})
	ctx := context.Background()
	e, err := s.Eligibility(ctx, "saavn:s1")
	if err != nil || !e.Eligible || e.Descriptor == nil || len(e.Descriptor.Sources) == 0 {
		t.Fatalf("eligibility: %+v %v", e, err)
	}
	step := func(u Update) error {
		_, err := s.Report(ctx, d, "saavn:s1", u)
		return err
	}
	for _, u := range []Update{
		{Status: StatusQueued},
		{Status: StatusDownloading, BytesTotal: 100, BytesDone: 10},
		{Status: StatusPaused, BytesTotal: 100, BytesDone: 40},
		{Status: StatusDownloading, BytesTotal: 100, BytesDone: 100},
		{Status: StatusVerifying, BytesTotal: 100, BytesDone: 100},
	} {
		if err := step(u); err != nil {
			t.Fatalf("%s: %v", u.Status, err)
		}
	}
	if err := step(Update{Status: StatusCompleted, BytesTotal: 100, BytesDone: 100}); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("completion without checksum accepted")
	}
	if err := step(Update{Status: StatusCompleted, BytesTotal: 100, BytesDone: 99, SHA256: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("completion with size mismatch accepted")
	}
	if err := step(Update{Status: StatusCompleted, BytesTotal: 100, BytesDone: 100, SHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ctx, d)
	if len(list) != 1 || list[0].Status != StatusCompleted || list[0].LocalID != LocalID("saavn:s1") {
		t.Fatalf("list: %+v", list)
	}
	// Missing/corrupt file found later.
	if err := step(Update{Status: StatusFailed, Error: "file missing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, d, "saavn:s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, d, "saavn:s1"); catalog.KindOf(err) != catalog.KindNotFound {
		t.Fatal("double remove")
	}
}

func TestLocalIDStable(t *testing.T) {
	if LocalID("saavn:s1") != LocalID("saavn:s1") || LocalID("saavn:s1") == LocalID("saavn:s2") {
		t.Fatal("local ids must be stable and distinct")
	}
}
