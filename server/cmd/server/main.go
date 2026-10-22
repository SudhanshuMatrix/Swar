// Command server runs the Swar backend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"swar/internal/auth"
	"swar/internal/cache"
	"swar/internal/catalog"
	"swar/internal/config"
	"swar/internal/downloads"
	"swar/internal/httpapi"
	"swar/internal/jam"
	"swar/internal/library"
	"swar/internal/music"
	"swar/internal/music/providers"
	"swar/internal/observability"
	"swar/internal/playback"
	"swar/internal/providers/deepsearch"
	"swar/internal/radio"
	"swar/internal/recommendations"
	"swar/internal/repository"
	"swar/internal/search"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(log)
	if os.Getenv("SWAR_TOKEN_SECRET") == "" {
		log.Warn("SWAR_TOKEN_SECRET is not set; using a random development secret (device tokens reset on restart)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, store, err := build(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer store.Close()

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// No WriteTimeout: Jam WebSockets are long-lived. Per-request
		// deadlines are applied by middleware instead.
	}

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go srv.Background(bgCtx)

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "env", cfg.Env,
			"deezer", cfg.MusicProviderDeezerEnabled, "itunes", cfg.MusicProviderITunesEnabled,
			"musicbrainz", cfg.MusicProviderMusicBrainzEnabled, "deep_search", cfg.DeepSearchEnabled,
			"deep_search_playback", cfg.DeepSearchPlayback)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	bgCancel()
	return httpServer.Shutdown(shutdownCtx)
}

// build wires every component together.
func build(ctx context.Context, cfg *config.Config, log *slog.Logger) (*httpapi.Server, *repository.Store, error) {
	metrics := observability.NewMetrics()

	store, err := repository.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}

	musicEngine := music.NewEngine(music.EngineOptions{
		DeezerEnabled:       cfg.MusicProviderDeezerEnabled,
		ITunesEnabled:       cfg.MusicProviderITunesEnabled,
		MusicBrainzEnabled:  cfg.MusicProviderMusicBrainzEnabled,
		SpotifyEnabled:      cfg.MusicProviderSpotifyEnabled,
		SpotifyClientID:     cfg.SpotifyClientID,
		SpotifyClientSecret: cfg.SpotifyClientSecret,
		SearchTimeout:       cfg.MusicSearchTimeout,
		MaxConcurrency:      cfg.MusicMaxConcurrency,
		ClientOptions: providers.ClientOptions{
			Timeout:        cfg.MusicSearchTimeout,
			MaxConcurrency: cfg.MusicMaxConcurrency,
		},
	})
	deep := deepsearch.New(deepsearch.Options{
		Binary: cfg.YTDLPPath, Timeout: cfg.DeepSearchTimeout, MaxResults: cfg.DeepSearchMaxResults,
		Concurrency: cfg.DeepSearchConcurrency, MaxOutput: cfg.DeepSearchMaxOutput,
		Playback: cfg.DeepSearchPlayback, Downloads: cfg.DeepSearchDownloads, Metrics: metrics,
	})

	c := cache.New(cfg.CacheMaxEntries)
	metrics.Gauge("cache_entries", func() float64 { return float64(c.Len()) })
	metrics.Gauge("cache_hits_total", func() float64 { return float64(c.Stats().Hits) })
	metrics.Gauge("cache_misses_total", func() float64 { return float64(c.Stats().Misses) })
	metrics.Gauge("cache_evictions_total", func() float64 { return float64(c.Stats().Evictions) })
	metrics.Gauge("cache_coalesced_total", func() float64 { return float64(c.Stats().Coalesced) })

	cat := catalog.NewService(musicEngine, c, catalog.DefaultTTLs)
	cat.RegisterSource(catalog.ProviderYouTube, deep)

	resolver := playback.NewResolver(cat, c, metrics)
	lib := library.New(store, cat)
	rec := recommendations.NewService(cat, lib)
	rad := radio.NewManager(cat, rec, lib, cfg.RadioSessionTTL)
	jams := jam.NewManager(jam.Options{
		MaxParticipants: cfg.JamMaxParticipants, MaxQueue: cfg.JamMaxQueue,
		IdleTTL: cfg.JamIdleTTL, MaxLifetime: cfg.JamMaxLifetime,
	}, cat, metrics)
	metrics.Gauge("jam_active", func() float64 { return float64(jams.ActiveCount()) })
	metrics.Gauge("radio_sessions", func() float64 { return float64(rad.Len()) })

	srv := httpapi.New(httpapi.Deps{
		Config: cfg, Log: log, Metrics: metrics, Catalog: cat, Search: search.New(cat), DeepSearch: deep,
		Playback: resolver, Recommendations: rec, Radio: rad, Library: lib,
		Downloads: downloads.New(store, cat, resolver), Jam: jams, Store: store,
		Tokens: auth.NewTokens(cfg.TokenSecret, cfg.TokenTTL),
	})
	return srv, store, nil
}
