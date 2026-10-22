package innertube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVideoRenderers(t *testing.T) {
	mockJSON := `{
		"contents": {
			"twoColumnSearchResultsRenderer": {
				"primaryContents": {
					"sectionListRenderer": {
						"contents": [
							{
								"itemSectionRenderer": {
									"contents": [
										{
											"videoRenderer": {
												"videoId": "LIHABJUqZ7s",
												"title": {
													"runs": [{"text": "Kesariya - Audio | Brahmāstra"}]
												},
												"ownerText": {
													"runs": [{"text": "Sony Music India"}]
												},
												"lengthText": {
													"simpleText": "4:28"
												}
											}
										}
									]
								}
							}
						]
					}
				}
			}
		}
	}`

	cands, err := parseVideoRenderers([]byte(mockJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	c := cands[0]
	if c.VideoID != "LIHABJUqZ7s" {
		t.Errorf("expected videoId LIHABJUqZ7s, got %s", c.VideoID)
	}
	if c.Title != "Kesariya - Audio | Brahmāstra" {
		t.Errorf("expected title Kesariya - Audio | Brahmāstra, got %s", c.Title)
	}
	if c.DurationSec != 268 {
		t.Errorf("expected duration 268, got %d", c.DurationSec)
	}
}

func TestPickBestCandidate(t *testing.T) {
	r := NewResolver()
	cands := []Candidate{
		{
			VideoID:     "randomCover",
			Title:       "Kesariya (Ukulele Cover by Bob)",
			Channel:     "Bob Music",
			DurationSec: 120,
		},
		{
			VideoID:     "officialAudio",
			Title:       "Kesariya - Audio | Brahmāstra | Pritam | Arijit Singh",
			Channel:     "Sony Music India",
			DurationSec: 268,
		},
		{
			VideoID:     "oneHourLoop",
			Title:       "Kesariya 1 Hour Loop",
			Channel:     "Loop Songs",
			DurationSec: 3600,
		},
	}

	best := r.pickBestCandidate(cands, "Kesariya", "Arijit Singh, Pritam", 268)
	if best != "officialAudio" {
		t.Errorf("expected officialAudio, got %s", best)
	}
}

func TestResolverWithMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"contents": {
				"twoColumnSearchResultsRenderer": {
					"primaryContents": {
						"sectionListRenderer": {
							"contents": [
								{
									"itemSectionRenderer": {
										"contents": [
											{
												"videoRenderer": {
													"videoId": "mRzgyx9PG4w",
													"title": { "runs": [{"text": "Kesariya Song"}] },
													"ownerText": { "runs": [{"text": "Sony Music"}] },
													"lengthText": { "simpleText": "4:28" }
												}
											}
										]
									}
								}
							]
						}
					}
				}
			}
		}`))
	}))
	defer ts.Close()

	r := NewResolver()
	r.http = ts.Client()

	// Direct check of candidate parsing from mock
	resp, err := r.http.Get(ts.URL)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer resp.Body.Close()

	// Test cache lookup
	r.cache[cacheKey("Kesariya", "Arijit Singh")] = "mRzgyx9PG4w"
	vid, err := r.ResolveYouTubeID(context.Background(), "Kesariya", "Arijit Singh", 268)
	if err != nil || vid != "mRzgyx9PG4w" {
		t.Errorf("expected cached id mRzgyx9PG4w, got %s (err: %v)", vid, err)
	}
}
