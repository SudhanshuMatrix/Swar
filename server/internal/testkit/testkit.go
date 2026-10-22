// Package testkit wires real components against fakes for tests.
package testkit

import (
	"context"
	"testing"

	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/repository"
)

type Options struct {
	MediaURLs bool
	Downloads bool
}

// Catalog returns a catalog service backed by an in-memory fake catalog.
func Catalog(t testing.TB, o Options) (*catalog.Service, *Fake) {
	t.Helper()
	f := NewFake(o)
	t.Cleanup(f.Close)
	return catalog.NewService(f, cache.New(1000), catalog.DefaultTTLs), f
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
