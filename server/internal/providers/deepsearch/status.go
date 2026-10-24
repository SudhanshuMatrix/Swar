package deepsearch

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"swar/internal/catalog"
)

// Status describes Deep Search for GET /v1/search/deep/status. It never
// includes paths, arguments or upstream error text.
type Status struct {
	Playback  bool   `json:"playback"`
	Downloads bool   `json:"downloads"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Running   int    `json:"running"`
	Capacity  int    `json:"capacity"`
}

const versionTTL = 10 * time.Minute

// Status reports whether yt-dlp runs and how busy it is. The version check
// runs yt-dlp at most once per ten minutes.
func (s *Service) Status(ctx context.Context) Status {
	st := Status{
		Playback:  s.opt.Playback,
		Downloads: s.opt.Playback && s.opt.Downloads,
		Running:   int(s.active.Load()),
		Capacity:  cap(s.sem),
	}
	v, err := s.Version(ctx)
	switch {
	case err == nil:
		st.Available, st.Version = true, v
	case errors.Is(err, errNotInstalled):
		st.Reason = "yt-dlp is not installed on this server"
	default:
		st.Reason = "yt-dlp is installed but did not start"
	}
	return st
}

// Version runs "yt-dlp --version" (cached for ten minutes). Shared and
// bounded like any other run, but it does not take a search slot: it is
// short and must answer even while every slot is busy.
func (s *Service) Version(ctx context.Context) (string, error) {
	s.versionMu.Lock()
	if !s.versionAt.IsZero() && time.Since(s.versionAt) < versionTTL {
		v, err := s.version, s.versionErr
		s.versionMu.Unlock()
		return v, err
	}
	s.versionMu.Unlock()
	v, err := shared(ctx, s, "deepsearch:yt:version", func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		out, err := s.exec(ctx, []string{"--ignore-config", "--version"}, 4<<10)
		if err != nil {
			return "", err
		}
		return cleanVersion(string(out)), nil
	})
	if catalog.KindOf(err) == catalog.KindRateLimited {
		return "", err // the caller gave up; do not remember that
	}
	s.versionMu.Lock()
	s.version, s.versionErr, s.versionAt = v, err, time.Now()
	s.versionMu.Unlock()
	return v, err
}

// ForgetVersion makes the next Status ask yt-dlp again (after an update).
func (s *Service) ForgetVersion() {
	s.versionMu.Lock()
	s.versionAt = time.Time{}
	s.versionMu.Unlock()
}

// cleanVersion keeps the first line, only version-like characters, at most
// 40 of them: the output is shown to clients.
func cleanVersion(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	var b strings.Builder
	for _, r := range line {
		if b.Len() >= 40 {
			break
		}
		if unicode.IsDigit(r) || unicode.IsLetter(r) && r < unicode.MaxASCII || r == '.' || r == '-' || r == '+' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Running is the number of yt-dlp processes running now.
func (s *Service) Running() int { return int(s.active.Load()) }
