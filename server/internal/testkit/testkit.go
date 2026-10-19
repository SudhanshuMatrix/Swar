// Package testkit wires real components against fakes for tests.
package testkit

import (
	"context"
	"testing"
	"time"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/providers/saavn"
	"swar/internal/providers/saavn/saavntest"
	"swar/internal/repository"
)

type Options struct {
	MediaURLs bool
	Downloads bool
}

// Catalog returns a catalog service backed by a fake Saavn server.
func Catalog(t testing.TB, o Options) (*catalog.Service, *saavntest.Fake) {
	t.Helper()
	f := saavntest.New()
	t.Cleanup(f.Close)
	c, err := saavn.New(saavn.Options{
		BaseURL: f.APIURL(), Timeout: 2 * time.Second, MaxConcurrency: 8, MaxRetries: 0,
		MaxBodyBytes: 1 << 20, MediaURLs: o.MediaURLs, Downloads: o.Downloads,
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog.NewService(c, cache.New(1000), catalog.DefaultTTLs), f
}

// Store returns an isolated in-memory database.
func Store(t testing.TB) *repository.Store {
	t.Helper()
	s, err := repository.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Device creates a device row and returns its ID.
func Device(t testing.TB, s *repository.Store, id string) string {
	t.Helper()
	if err := s.CreateDevice(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return id
}
