package innertube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"swar/internal/music/normalize"
)

var ytIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Candidate represents a YouTube candidate found via InnerTube search.
type Candidate struct {
	VideoID     string
	Title       string
	Channel     string
	DurationSec int
}

// Resolver resolves song metadata to matching YouTube video IDs via InnerTube's
// unauthenticated public endpoints (like OMMR's zero-key InnerTube client).
type Resolver struct {
	http    *http.Client
	mu      sync.RWMutex
	cache   map[string]string // normalized query -> videoID
	maxSize int
}

// NewResolver creates a new InnerTube resolver.
func NewResolver() *Resolver {
	return &Resolver{
		http: &http.Client{
			Timeout: 6 * time.Second,
		},
		cache:   make(map[string]string),
		maxSize: 5000,
	}
}

// cacheKey produces a deterministic key for title and artist.
func cacheKey(title, artist string) string {
	t := normalize.CleanTitle(title)
	a := normalize.FoldForComparison(artist)
	return strings.ToLower(t + "|" + a)
}

// ResolveYouTubeID finds the best matching YouTube video ID for a song.
func (r *Resolver) ResolveYouTubeID(ctx context.Context, title, artist string, durationSec int) (string, error) {
	key := cacheKey(title, artist)
	if key == "|" {
		return "", fmt.Errorf("empty title and artist")
	}

	r.mu.RLock()
	if id, ok := r.cache[key]; ok && id != "" {
		r.mu.RUnlock()
		return id, nil
	}
	r.mu.RUnlock()

	query := strings.TrimSpace(title + " " + artist)
	cands, err := r.SearchCandidates(ctx, query)
	if err != nil || len(cands) == 0 {
		// Try fallback query with "audio"
		cands, err = r.SearchCandidates(ctx, query+" audio")
		if err != nil || len(cands) == 0 {
			return "", fmt.Errorf("no youtube candidates found")
		}
	}

	bestID := r.pickBestCandidate(cands, title, artist, durationSec)
	if bestID == "" {
		bestID = cands[0].VideoID
	}

	r.mu.Lock()
	if len(r.cache) >= r.maxSize {
		r.cache = make(map[string]string)
	}
	r.cache[key] = bestID
	r.mu.Unlock()

	return bestID, nil
}

// ClearCache forgets every remembered match and returns how many there were.
func (r *Resolver) ClearCache() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.cache)
	r.cache = make(map[string]string)
	return n
}

// SearchCandidates queries YouTube's InnerTube search endpoint.
func (r *Resolver) SearchCandidates(ctx context.Context, query string) ([]Candidate, error) {
	payload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20230101.01.00",
				"hl":            "en",
			},
		},
		"query": query,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.youtube.com/youtubei/v1/search", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("innertube returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	return parseVideoRenderers(body)
}

func parseVideoRenderers(data []byte) ([]Candidate, error) {
	var root struct {
		Contents struct {
			TwoColumnSearchResultsRenderer struct {
				PrimaryContents struct {
					SectionListRenderer struct {
						Contents []struct {
							ItemSectionRenderer struct {
								Contents []struct {
									VideoRenderer *struct {
										VideoID string `json:"videoId"`
										Title   struct {
											Runs []struct {
												Text string `json:"text"`
											} `json:"runs"`
										} `json:"title"`
										OwnerText struct {
											Runs []struct {
												Text string `json:"text"`
											} `json:"runs"`
										} `json:"ownerText"`
										LengthText struct {
											SimpleText string `json:"simpleText"`
										} `json:"lengthText"`
									} `json:"videoRenderer"`
								} `json:"contents"`
							} `json:"itemSectionRenderer"`
						} `json:"contents"`
					} `json:"sectionListRenderer"`
				} `json:"primaryContents"`
			} `json:"twoColumnSearchResultsRenderer"`
		} `json:"contents"`
	}

	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, sec := range root.Contents.TwoColumnSearchResultsRenderer.PrimaryContents.SectionListRenderer.Contents {
		for _, item := range sec.ItemSectionRenderer.Contents {
			vr := item.VideoRenderer
			if vr == nil || !ytIDRe.MatchString(vr.VideoID) {
				continue
			}

			var title string
			for _, r := range vr.Title.Runs {
				title += r.Text
			}

			var channel string
			for _, r := range vr.OwnerText.Runs {
				channel += r.Text
			}

			dur := parseDurationText(vr.LengthText.SimpleText)

			candidates = append(candidates, Candidate{
				VideoID:     vr.VideoID,
				Title:       title,
				Channel:     channel,
				DurationSec: dur,
			})
			if len(candidates) >= 10 {
				break
			}
		}
	}

	return candidates, nil
}

func parseDurationText(text string) int {
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) == 2 {
		m, _ := strconv.Atoi(parts[0])
		s, _ := strconv.Atoi(parts[1])
		return m*60 + s
	}
	if len(parts) == 3 {
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		s, _ := strconv.Atoi(parts[2])
		return h*3600 + m*60 + s
	}
	return 0
}

func (r *Resolver) pickBestCandidate(cands []Candidate, targetTitle, targetArtist string, targetDur int) string {
	bestScore := -1.0
	bestID := ""

	normTargetTitle := normalize.CleanTitle(targetTitle)
	normTargetArtist := normalize.FoldForComparison(targetArtist)

	for _, c := range cands {
		normCandTitle := normalize.CleanTitle(c.Title)
		score := normalize.TokenSetRatio(normTargetTitle, normCandTitle) * 0.6

		// Check artist in title or channel
		if strings.Contains(strings.ToLower(c.Title), strings.ToLower(normTargetArtist)) ||
			strings.Contains(strings.ToLower(c.Channel), strings.ToLower(normTargetArtist)) {
			score += 0.2
		}

		// Prefer official audio / topic channel
		lowerTitle := strings.ToLower(c.Title)
		lowerChan := strings.ToLower(c.Channel)
		if strings.Contains(lowerTitle, "audio") || strings.Contains(lowerTitle, "official") || strings.Contains(lowerChan, "topic") {
			score += 0.1
		}

		// Penalize large duration discrepancies
		if targetDur > 0 && c.DurationSec > 0 {
			diff := abs(c.DurationSec - targetDur)
			if diff <= 8 {
				score += 0.2
			} else if diff <= 20 {
				score += 0.1
			} else if diff > 60 {
				score -= 0.3
			}
		}

		if score > bestScore {
			bestScore = score
			bestID = c.VideoID
		}
	}

	return bestID
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
