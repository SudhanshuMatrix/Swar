package providers

import (
	"context"
	"net/http"
	"time"

	"swar/internal/catalog"
)

// Provider defines the interface implemented by each native metadata provider
// (Deezer, iTunes, MusicBrainz, Spotify).
type Provider interface {
	Name() string
	SearchTracks(ctx context.Context, query string, limit int) ([]catalog.Track, error)
	SearchAlbums(ctx context.Context, query string, limit int) ([]catalog.Album, error)
	SearchArtists(ctx context.Context, query string, limit int) ([]catalog.ArtistRef, error)
	SearchPlaylists(ctx context.Context, query string, limit int) ([]catalog.Playlist, error)
	Track(ctx context.Context, id string) (*catalog.Track, error)
	Album(ctx context.Context, id string) (*catalog.Album, error)
	Artist(ctx context.Context, id string) (*catalog.Artist, error)
	ArtistTopTracks(ctx context.Context, id string, limit int) ([]catalog.Track, error)
	ArtistAlbums(ctx context.Context, id string, limit int) ([]catalog.Album, error)
	Playlist(ctx context.Context, id string, limit int) (*catalog.Playlist, error)
	Trending(ctx context.Context, language string) ([]catalog.Collection, error)
	Browse(ctx context.Context, language string) ([]catalog.Collection, error)
}

// ClientOptions provides standard HTTP client settings across all providers.
type ClientOptions struct {
	HTTPClient     *http.Client
	Timeout        time.Duration
	MaxConcurrency int
	MaxRetries     int
	MaxBodyBytes   int64
	UserAgent      string
}
