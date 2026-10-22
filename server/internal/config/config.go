// Package config loads and validates the server configuration from
// environment variables. Every setting has a safe default for local
// development; production deployments must set SWAR_TOKEN_SECRET.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string // "development" or "production"
	Addr     string
	LogLevel string

	// CORSOrigins is the list of allowed browser origins; "*" allows any.
	CORSOrigins []string
	// TrustProxy makes the rate limiter use X-Forwarded-For. Only enable it
	// behind a reverse proxy that overwrites that header.
	TrustProxy bool

	DBPath      string
	TokenSecret []byte
	TokenTTL    time.Duration

	MaxBodyBytes    int64
	RequestTimeout  time.Duration
	RateLimitRPS    float64
	RateLimitBurst  int
	CacheMaxEntries int

	MusicProviderDeezerEnabled      bool
	MusicProviderITunesEnabled      bool
	MusicProviderMusicBrainzEnabled bool
	MusicProviderSpotifyEnabled     bool

	SpotifyClientID     string
	SpotifyClientSecret string

	MusicSearchTimeout  time.Duration
	MusicMaxConcurrency int

	YTDLPPath             string
	DeepSearchEnabled     bool
	DeepSearchTimeout     time.Duration
	DeepSearchMaxResults  int
	DeepSearchConcurrency int
	DeepSearchMaxOutput   int64
	// DeepSearchPlayback allows resolving a deep-search candidate into a
	// playable stream. Off by default: see docs/DECISIONS.md.
	DeepSearchPlayback  bool
	DeepSearchDownloads bool

	JamMaxParticipants int
	JamMaxQueue        int
	JamIdleTTL         time.Duration
	JamMaxLifetime     time.Duration

	RadioSessionTTL time.Duration

	// AdminUsername and AdminPassword protect /v1/admin (maintenance mode
	// and popups). The admin API stays off unless both are set.
	AdminUsername string
	AdminPassword string
}

// AdminEnabled reports whether the admin API is configured.
func (c *Config) AdminEnabled() bool { return c.AdminUsername != "" && c.AdminPassword != "" }

// Load reads the configuration from the environment.
func Load() (*Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom reads the configuration using getenv, which makes it testable.
func LoadFrom(getenv func(string) string) (*Config, error) {
	e := envReader{get: getenv}
	c := &Config{
		Env: e.str("SWAR_ENV", "development"),
		Addr: func() string {
			if a := e.str("SWAR_ADDR", ""); a != "" {
				return a
			}
			if p := e.str("PORT", ""); p != "" {
				return ":" + p
			}
			return ":8765"
		}(),
		LogLevel: e.str("SWAR_LOG_LEVEL", "info"),

		CORSOrigins: e.list("SWAR_CORS_ORIGINS", []string{"*"}),
		TrustProxy:  e.bool("SWAR_TRUST_PROXY", false),

		DBPath:   e.str("SWAR_DB_PATH", "data/swar.db"),
		TokenTTL: e.dur("SWAR_TOKEN_TTL", 365*24*time.Hour),

		MaxBodyBytes:    e.int64("SWAR_MAX_BODY_BYTES", 64<<10),
		RequestTimeout:  e.dur("SWAR_REQUEST_TIMEOUT", 30*time.Second),
		RateLimitRPS:    e.float("SWAR_RATE_LIMIT_RPS", 10),
		RateLimitBurst:  e.int("SWAR_RATE_LIMIT_BURST", 40),
		CacheMaxEntries: e.int("SWAR_CACHE_MAX_ENTRIES", 20000),

		MusicProviderDeezerEnabled:      e.bool("MUSIC_PROVIDER_DEEZER_ENABLED", true),
		MusicProviderITunesEnabled:      e.bool("MUSIC_PROVIDER_ITUNES_ENABLED", true),
		MusicProviderMusicBrainzEnabled: e.bool("MUSIC_PROVIDER_MUSICBRAINZ_ENABLED", true),
		MusicProviderSpotifyEnabled:     e.bool("MUSIC_PROVIDER_SPOTIFY_ENABLED", false),

		SpotifyClientID:     e.str("SPOTIFY_CLIENT_ID", ""),
		SpotifyClientSecret: getenv("SPOTIFY_CLIENT_SECRET"),

		MusicSearchTimeout:  e.dur("MUSIC_SEARCH_TIMEOUT", 8*time.Second),
		MusicMaxConcurrency: e.int("MUSIC_MAX_CONCURRENCY", 10),

		YTDLPPath:             e.str("YTDLP_PATH", "yt-dlp"),
		DeepSearchEnabled:     e.bool("DEEPSEARCH_ENABLED", true),
		DeepSearchTimeout:     e.dur("DEEPSEARCH_TIMEOUT", 25*time.Second),
		DeepSearchMaxResults:  e.int("DEEPSEARCH_MAX_RESULTS", 10),
		DeepSearchConcurrency: e.int("DEEPSEARCH_CONCURRENCY", 2),
		DeepSearchMaxOutput:   e.int64("DEEPSEARCH_MAX_OUTPUT_BYTES", 8<<20),
		DeepSearchPlayback:    e.bool("DEEPSEARCH_PLAYBACK_ENABLED", false),
		DeepSearchDownloads:   e.bool("DEEPSEARCH_DOWNLOADS_ENABLED", false),

		JamMaxParticipants: e.int("JAM_MAX_PARTICIPANTS", 20),
		JamMaxQueue:        e.int("JAM_MAX_QUEUE", 200),
		JamIdleTTL:         e.dur("JAM_IDLE_TTL", 30*time.Minute),
		JamMaxLifetime:     e.dur("JAM_MAX_LIFETIME", 12*time.Hour),

		RadioSessionTTL: e.dur("RADIO_SESSION_TTL", 6*time.Hour),

		AdminUsername: e.str("ADMIN_USERNAME", ""),
		AdminPassword: getenv("ADMIN_PASSWORD"),
	}
	if len(e.errs) > 0 {
		return nil, errors.Join(e.errs...)
	}

	secret := getenv("SWAR_TOKEN_SECRET")
	switch {
	case secret != "":
		if len(secret) < 32 {
			return nil, errors.New("SWAR_TOKEN_SECRET must be at least 32 characters")
		}
		c.TokenSecret = []byte(secret)
	case c.IsProduction():
		return nil, errors.New("SWAR_TOKEN_SECRET is required when SWAR_ENV=production")
	default:
		// Development only: tokens stop working after a restart.
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		c.TokenSecret = []byte(hex.EncodeToString(b))
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func (c *Config) validate() error {
	var errs []error
	check := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, errors.New(msg))
		}
	}
	check(c.Env == "development" || c.Env == "production", "SWAR_ENV must be development or production")
	check(c.MaxBodyBytes > 0 && c.MaxBodyBytes <= 10<<20, "SWAR_MAX_BODY_BYTES must be between 1 and 10MiB")
	check(c.RateLimitRPS > 0 && c.RateLimitBurst > 0, "rate limit values must be positive")
	check(c.CacheMaxEntries >= 100, "SWAR_CACHE_MAX_ENTRIES must be at least 100")
	check(c.MusicMaxConcurrency >= 1 && c.MusicMaxConcurrency <= 64, "MUSIC_MAX_CONCURRENCY must be 1-64")
	check(c.DeepSearchMaxResults >= 1 && c.DeepSearchMaxResults <= 25, "DEEPSEARCH_MAX_RESULTS must be 1-25")
	check(c.DeepSearchConcurrency >= 1 && c.DeepSearchConcurrency <= 8, "DEEPSEARCH_CONCURRENCY must be 1-8")
	check(c.JamMaxParticipants >= 2 && c.JamMaxParticipants <= 100, "JAM_MAX_PARTICIPANTS must be 2-100")
	check(c.JamMaxQueue >= 10 && c.JamMaxQueue <= 1000, "JAM_MAX_QUEUE must be 10-1000")
	return errors.Join(errs...)
}

type envReader struct {
	get  func(string) string
	errs []error
}

func (e *envReader) str(key, def string) string {
	if v := strings.TrimSpace(e.get(key)); v != "" {
		return v
	}
	return def
}

func (e *envReader) list(key string, def []string) []string {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (e *envReader) bool(key string, def bool) bool {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return b
}

func (e *envReader) int(key string, def int) int {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return n
}

func (e *envReader) int64(key string, def int64) int64 {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return n
}

func (e *envReader) float(key string, def float64) float64 {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return n
}

func (e *envReader) dur(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return d
}
