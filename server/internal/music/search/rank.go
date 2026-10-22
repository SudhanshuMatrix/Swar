package search

import (
	"math"
	"sort"
	"strings"

	"swar/internal/catalog"
	"swar/internal/music/normalize"
)

// Provider trust coefficients (calibrated weights)
var providerWeights = map[string]float64{
	"it":    1.15, // Apple iTunes: clean official metadata and high-res artwork
	"dz":    1.10, // Deezer: strong metadata and preview streams
	"sp":    1.10, // Spotify: good metadata
	"mb":    1.05, // MusicBrainz: open encyclopedia, reliable ISRC and credits
	"yt":    0.90, // DeepSearch / YouTube: higher variance in titles
	"saavn": 1.00, // Legacy Saavn
}

// RankTracks orders tracks deterministically by their relevance to the search query.
func RankTracks(tracks []catalog.Track, query string) []catalog.Track {
	if len(tracks) <= 1 {
		return tracks
	}

	qFold := normalize.FoldForComparison(query)
	qTags := normalize.ExtractVariationTags(query)
	qTokens := strings.Fields(qFold)

	type scoredTrack struct {
		track catalog.Track
		score float64
	}

	scored := make([]scoredTrack, len(tracks))
	for i, t := range tracks {
		score := scoreTrack(t, qFold, qTokens, qTags)
		scored[i] = scoredTrack{track: t, score: score}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		// 1. Highest relevance score first
		if math.Abs(scored[i].score-scored[j].score) > 0.001 {
			return scored[i].score > scored[j].score
		}
		// 2. Playable preview over non-playable
		if scored[i].track.Playable != scored[j].track.Playable {
			return scored[i].track.Playable
		}
		// 3. Higher popularity first
		if scored[i].track.PlayCount != scored[j].track.PlayCount {
			return scored[i].track.PlayCount > scored[j].track.PlayCount
		}
		// 4. Stable alphabetical title
		if scored[i].track.Title != scored[j].track.Title {
			return scored[i].track.Title < scored[j].track.Title
		}
		// 5. Stable ID
		return scored[i].track.ID < scored[j].track.ID
	})

	out := make([]catalog.Track, len(scored))
	for i, s := range scored {
		out[i] = s.track
	}
	return out
}

func scoreTrack(t catalog.Track, qFold string, qTokens []string, qTags normalize.VariationTags) float64 {
	score := 0.0
	titleFold := normalize.FoldForComparison(t.Title)
	artistFold := normalize.FoldForComparison(t.ArtistNames())

	// 1. Exact title match
	if titleFold == qFold {
		score += 100.0
	} else if strings.HasPrefix(titleFold, qFold) {
		score += 65.0
	} else if strings.Contains(titleFold, qFold) {
		score += 45.0
	}

	// 2. Jaro-Winkler & TokenSet similarity
	titleSim := normalize.BestMatchScore(titleFold, qFold)
	score += titleSim * 40.0

	// 3. Token containment (all query tokens in title or artist)
	tokensMatched := 0
	for _, tok := range qTokens {
		if strings.Contains(titleFold, tok) || strings.Contains(artistFold, tok) {
			tokensMatched++
		}
	}
	if len(qTokens) > 0 {
		score += (float64(tokensMatched) / float64(len(qTokens))) * 30.0
	}

	// 4. Artist match
	if artistFold != "" && (artistFold == qFold || strings.Contains(artistFold, qFold)) {
		score += 35.0
	}

	// 5. Variation alignment:
	// If query didn't ask for a remix, live, acoustic, cover, or lofi, penalize them!
	// If query explicitly asked for one, reward them.
	tTags := normalize.ExtractVariationTags(t.Title)
	if !qTags.IsRemix && tTags.IsRemix {
		score -= 25.0
	} else if qTags.IsRemix && tTags.IsRemix {
		score += 25.0
	}

	if !qTags.IsLive && tTags.IsLive {
		score -= 20.0
	} else if qTags.IsLive && tTags.IsLive {
		score += 20.0
	}

	if !qTags.IsAcoustic && tTags.IsAcoustic {
		score -= 15.0
	} else if qTags.IsAcoustic && tTags.IsAcoustic {
		score += 20.0
	}

	if !qTags.IsCover && tTags.IsCover {
		score -= 30.0
	} else if qTags.IsCover && tTags.IsCover {
		score += 25.0
	}

	if !qTags.IsLofi && tTags.IsLofi {
		score -= 20.0
	} else if qTags.IsLofi && tTags.IsLofi {
		score += 20.0
	}

	// 6. Provider trust weight
	weight := providerWeights[t.Provider]
	if weight <= 0 {
		weight = 1.0
	}
	score *= weight

	// 7. Popularity signal (logarithmic bonus)
	if t.PlayCount > 0 {
		bonus := math.Log10(float64(t.PlayCount) + 1.0)
		score += math.Min(bonus*2.0, 15.0)
	}

	return score
}
