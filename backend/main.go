// Sonivo backend — resolves search results and playable stream URLs by
// shelling out to the yt-dlp CLI binary.
//
// This exists only because yt-dlp cannot run inside a Flutter Web page or an
// iOS app sandbox. It is intentionally thin: it never proxies audio/video
// bytes. Every endpoint just returns metadata plus a direct CDN URL, which
// the Flutter client then streams from directly. Standard library only, no
// third-party Go dependencies.
//
// Requires the `yt-dlp` executable to be installed and on PATH (or point
// YT_DLP_PATH at it). Install it with `pip install -U yt-dlp` or grab the
// standalone binary from https://github.com/yt-dlp/yt-dlp/releases.
//
// Run with: make backend   (see repository root Makefile), or directly:
//   go run ./backend
//
// For personal / educational use only. Resolving and streaming third-party
// video-site media may be subject to that site's Terms of Service in your
// jurisdiction — you are responsible for how you use this.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 3 * time.Hour // comfortably under YouTube's stream-URL expiry

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func ytdlpPath() string {
	if p := os.Getenv("YT_DLP_PATH"); p != "" {
		return p
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	if p, err := exec.LookPath("yt-dlp.exe"); err == nil {
		return p
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		matches, _ := filepath.Glob(filepath.Join(appData, "Python", "Python*", "Scripts", "yt-dlp.exe"))
		if len(matches) > 0 {
			return matches[len(matches)-1]
		}
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		matches, _ := filepath.Glob(filepath.Join(localAppData, "Programs", "Python", "Python*", "Scripts", "yt-dlp.exe"))
		if len(matches) > 0 {
			return matches[len(matches)-1]
		}
	}
	for _, p := range []string{
		`C:\Python314\Scripts\yt-dlp.exe`,
		`C:\Python313\Scripts\yt-dlp.exe`,
		`C:\Python312\Scripts\yt-dlp.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := exec.LookPath("python"); err == nil {
		return "python-m"
	}
	return "yt-dlp"
}

// ---- yt-dlp JSON shapes (only the fields we need) ----

type format struct {
	URL    string  `json:"url"`
	Acodec string  `json:"acodec"`
	Vcodec string  `json:"vcodec"`
	Height float64 `json:"height"`
	ABR    float64 `json:"abr"`
}

type thumbnail struct {
	URL string `json:"url"`
}

type ytInfo struct {
	ID               string      `json:"id"`
	Title            string      `json:"title"`
	Uploader         string      `json:"uploader"`
	Channel          string      `json:"channel"`
	Thumbnail        string      `json:"thumbnail"`
	Thumbnails       []thumbnail `json:"thumbnails"`
	Duration         float64     `json:"duration"`
	URL              string      `json:"url"`
	RequestedFormats []format    `json:"requested_formats"`
	Formats          []format    `json:"formats"`
}

func (i ytInfo) artist() string {
	if i.Uploader != "" {
		return i.Uploader
	}
	return i.Channel
}

func (i ytInfo) bestThumbnail() string {
	if len(i.Thumbnails) > 0 {
		return i.Thumbnails[len(i.Thumbnails)-1].URL
	}
	return i.Thumbnail
}

func (i ytInfo) pickURL(wantVideo bool) string {
	if i.URL != "" {
		return i.URL
	}
	candidates := append(append([]format{}, i.RequestedFormats...), i.Formats...)
	filtered := make([]format, 0, len(candidates))
	for _, f := range candidates {
		if f.URL == "" {
			continue
		}
		// Modern YouTube rarely serves "progressive" formats (one URL with
		// both tracks muxed) any more, so video mode intentionally resolves
		// a video-only stream — the Flutter client pairs it with the
		// separately-resolved audio-only stream and keeps both in sync
		// (muted video layer over the real audio engine). See /video below.
		if wantVideo {
			if f.Vcodec != "none" {
				filtered = append(filtered, f)
			}
		} else if f.Acodec != "none" {
			filtered = append(filtered, f)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	if wantVideo {
		sort.Slice(filtered, func(a, b int) bool { return filtered[a].Height > filtered[b].Height })
	} else {
		sort.Slice(filtered, func(a, b int) bool { return filtered[a].ABR > filtered[b].ABR })
	}
	return filtered[0].URL
}

// ---- tiny TTL cache so repeated seeks/toggles don't re-invoke yt-dlp ----

type cacheEntry struct {
	info    ytInfo
	expires time.Time
}

type ttlCache struct {
	mu    sync.Mutex
	items map[string]cacheEntry
}

func newTTLCache() *ttlCache { return &ttlCache{items: map[string]cacheEntry{}} }

func (c *ttlCache) get(key string) (ytInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.expires) {
		return ytInfo{}, false
	}
	return e.info, true
}

func (c *ttlCache) set(key string, info ytInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry{info: info, expires: time.Now().Add(cacheTTL)}
}

var resolveCache = newTTLCache()

// ---- yt-dlp invocation ----

func runYtDlp(ctx context.Context, args ...string) ([]byte, error) {
	bin := ytdlpPath()
	var cmd *exec.Cmd
	if bin == "python-m" {
		cmd = exec.CommandContext(ctx, "python", append([]string{"-m", "yt_dlp"}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, bin, args...)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("yt-dlp failed: %w (is yt-dlp installed and on PATH?)", err)
	}
	return out, nil
}

func extract(ctx context.Context, videoID string, wantVideo bool) (ytInfo, error) {
	cacheKey := videoID
	if wantVideo {
		cacheKey += "|video"
	} else {
		cacheKey += "|audio"
	}
	if cached, ok := resolveCache.get(cacheKey); ok {
		return cached, nil
	}

	format := "bestaudio[ext=m4a]/bestaudio/best"
	if wantVideo {
		// Video-only, capped at 1080p by default (plenty for a phone/laptop
		// screen and far less bandwidth than true "best"). The client plays
		// this muted alongside the real audio-only stream.
		format = "bestvideo[ext=mp4][height<=1080]/bestvideo[height<=1080]/bestvideo/best"
	}

	out, err := runYtDlp(ctx,
		"-f", format,
		"-j",
		"--no-warnings",
		"--no-playlist",
		"https://www.youtube.com/watch?v="+videoID,
	)
	if err != nil {
		return ytInfo{}, err
	}

	var info ytInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return ytInfo{}, fmt.Errorf("could not parse yt-dlp output: %w", err)
	}
	resolveCache.set(cacheKey, info)
	return info, nil
}

// ---- HTTP handlers ----

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type searchResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Thumbnail string `json:"thumbnail"`
	Duration  int    `json:"duration"`
}

var (
	ytUrlPattern = regexp.MustCompile(`(?:https?://)?(?:www\.|music\.)?(?:youtube\.com/watch\?v=|youtu\.be/)([A-Za-z0-9_-]{11})`)
	rawIdPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

func handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []searchResult{}})
		return
	}
	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// Direct YouTube URL or raw 11-char video ID lookup
	var directID string
	if m := ytUrlPattern.FindStringSubmatch(q); len(m) > 1 {
		directID = m[1]
	} else if rawIdPattern.MatchString(q) {
		directID = q
	}

	if directID != "" {
		info, err := extract(ctx, directID, false)
		if err == nil && info.ID != "" {
			title := info.Title
			if title == "" {
				title = "Unknown title"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"results": []searchResult{
					{
						ID:        info.ID,
						Title:     title,
						Artist:    info.artist(),
						Thumbnail: info.bestThumbnail(),
						Duration:  int(info.Duration),
					},
				},
			})
			return
		}
	}

	out, err := runYtDlp(ctx,
		"--flat-playlist",
		"-j",
		"--no-warnings",
		fmt.Sprintf("ytsearch%d:%s", limit, q),
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search failed: "+err.Error())
		return
	}

	results := make([]searchResult, 0, limit)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var info ytInfo
		if err := json.Unmarshal(scanner.Bytes(), &info); err != nil || info.ID == "" {
			continue
		}
		title := info.Title
		if title == "" {
			title = "Unknown title"
		}
		results = append(results, searchResult{
			ID:        info.ID,
			Title:     title,
			Artist:    info.artist(),
			Thumbnail: info.bestThumbnail(),
			Duration:  int(info.Duration),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func handleRelated(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("id")
	if !videoIDPattern.MatchString(videoID) {
		writeError(w, http.StatusBadRequest, "invalid video id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	info, err := extract(ctx, videoID, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, "resolve base track failed: "+err.Error())
		return
	}

	searchQuery := info.artist()
	if searchQuery == "" {
		searchQuery = info.Title
	}

	out, err := runYtDlp(ctx,
		"--flat-playlist",
		"-j",
		"--no-warnings",
		fmt.Sprintf("ytsearch12:%s", searchQuery),
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "related search failed: "+err.Error())
		return
	}

	results := make([]searchResult, 0, 10)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var item ytInfo
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil || item.ID == "" || item.ID == videoID {
			continue
		}
		title := item.Title
		if title == "" {
			title = "Unknown title"
		}
		results = append(results, searchResult{
			ID:        item.ID,
			Title:     title,
			Artist:    item.artist(),
			Thumbnail: item.bestThumbnail(),
			Duration:  int(item.Duration),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

type streamResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Thumbnail string `json:"thumbnail"`
	Duration  int    `json:"duration"`
	AudioURL  string `json:"audio_url,omitempty"`
	VideoURL  string `json:"video_url,omitempty"`
}

// handleResolve serves /track/{id} (audio only) and /video/{id}. Because
// YouTube rarely offers a single combined stream any more, /video/{id}
// additionally resolves and returns audio_url: the client plays the real
// audio from that URL and overlays the (muted) video_url stream in sync,
// rather than expecting one URL with both tracks muxed.
func handleResolve(wantVideo bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		videoID := r.PathValue("id")
		if !videoIDPattern.MatchString(videoID) {
			writeError(w, http.StatusBadRequest, "invalid video id")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		audioInfo, err := extract(ctx, videoID, false)
		if err != nil {
			writeError(w, http.StatusBadGateway, "resolve failed: "+err.Error())
			return
		}
		audioURL := audioInfo.pickURL(false)
		if audioURL == "" {
			writeError(w, http.StatusNotFound, "no playable audio stream found")
			return
		}

		title := audioInfo.Title
		if title == "" {
			title = "Unknown title"
		}
		resp := streamResponse{
			ID:        videoID,
			Title:     title,
			Artist:    audioInfo.artist(),
			Thumbnail: audioInfo.bestThumbnail(),
			Duration:  int(audioInfo.Duration),
			AudioURL:  audioURL,
		}

		if wantVideo {
			videoInfo, err := extract(ctx, videoID, true)
			if err != nil {
				writeError(w, http.StatusBadGateway, "resolve failed: "+err.Error())
				return
			}
			videoURL := videoInfo.pickURL(true)
			if videoURL == "" {
				writeError(w, http.StatusNotFound, "no playable video stream found")
				return
			}
			resp.VideoURL = videoURL
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", withCORS(handleHealth))
	mux.HandleFunc("/search", withCORS(handleSearch))
	mux.HandleFunc("/track/{id}", withCORS(handleResolve(false)))
	mux.HandleFunc("/video/{id}", withCORS(handleResolve(true)))
	mux.HandleFunc("/related/{id}", withCORS(handleRelated))

	addr := ":8765"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("Sonivo backend listening on %s (yt-dlp: %s)", addr, ytdlpPath())
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
