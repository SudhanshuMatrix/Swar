package search

import (
	"strings"

	"swar/internal/catalog"
	"swar/internal/music/normalize"
)

// DeduplicateAndMergeTracks takes a list of tracks from multiple providers,
// identifies identical recordings, and synthesizes them into canonical records.
// Different versions (Remixes, Live recordings, Covers, Acoustic versions, Lo-Fi)
// are strictly preserved and never merged.
func DeduplicateAndMergeTracks(tracks []catalog.Track) []catalog.Track {
	if len(tracks) <= 1 {
		return tracks
	}

	var canonical []catalog.Track

	for _, incoming := range tracks {
		matchedIndex := -1

		for i, existing := range canonical {
			if isSameRecording(incoming, existing) {
				matchedIndex = i
				break
			}
		}

		if matchedIndex >= 0 {
			// Merge incoming track into existing canonical track
			canonical[matchedIndex] = mergeTracks(canonical[matchedIndex], incoming)
		} else {
			canonical = append(canonical, incoming)
		}
	}

	return canonical
}

// isSameRecording decides whether t1 and t2 represent the exact same recording.
func isSameRecording(t1, t2 catalog.Track) bool {
	// 1. Same internal ID is always a match
	if t1.ID != "" && t1.ID == t2.ID {
		return true
	}

	// 2. Different versions (remix, live, acoustic, cover, instrumental, lofi) must NEVER be merged!
	if !normalize.AreCompatibleVersions(t1.Title, t2.Title) {
		return false
	}

	// 3. ISRC match (International Standard Recording Code uniquely identifies a sound recording)
	isrc1 := t1.ProviderMeta["isrc"]
	isrc2 := t2.ProviderMeta["isrc"]
	if isrc1 != "" && isrc2 != "" && strings.EqualFold(isrc1, isrc2) {
		return true
	}

	// 4. Duration check: identical recordings from different providers typically differ by <= 3-4s
	if t1.DurationSec > 0 && t2.DurationSec > 0 {
		diff := abs(t1.DurationSec - t2.DurationSec)
		if diff > 4 {
			return false
		}
	}

	// 5. Title and Artist similarity
	titleScore := normalize.BestMatchScore(t1.Title, t2.Title)
	if titleScore < 0.85 {
		return false
	}

	artist1 := t1.ArtistNames()
	artist2 := t2.ArtistNames()
	if artist1 != "" && artist2 != "" {
		artistScore := normalize.BestMatchScore(artist1, artist2)
		if artistScore < 0.78 {
			return false
		}
	}

	return true
}

// mergeTracks synthesizes two matching tracks into one rich canonical track.
func mergeTracks(base, extra catalog.Track) catalog.Track {
	// 1. Title: prefer cleaner or non-empty
	if base.Title == "" || (len(extra.Title) > 0 && len(extra.Title) < len(base.Title) && !strings.Contains(extra.Title, "[")) {
		base.Title = extra.Title
	}

	// 2. Artists: prefer whichever has more credited artists
	if len(extra.Artists) > len(base.Artists) {
		base.Artists = extra.Artists
	}

	// 3. Album: prefer non-nil
	if base.Album == nil && extra.Album != nil {
		base.Album = extra.Album
	}

	// 4. Duration: keep whichever is valid
	if base.DurationSec <= 0 && extra.DurationSec > 0 {
		base.DurationSec = extra.DurationSec
	}

	// 5. Images: prefer higher resolution set (Apple Music 1000x1000 or Deezer XL)
	if len(extra.Images) > len(base.Images) {
		base.Images = extra.Images
	}

	// 6. Playability: if either provider offers a playable preview stream, the track is playable
	if !base.Playable && extra.Playable {
		base.Playable = true
	}

	// 7. Popularity / PlayCount
	if extra.PlayCount > base.PlayCount {
		base.PlayCount = extra.PlayCount
	}

	// 8. Merge ProviderMeta cross-references
	if base.ProviderMeta == nil {
		base.ProviderMeta = make(map[string]string)
	}
	for k, v := range extra.ProviderMeta {
		if _, exists := base.ProviderMeta[k]; !exists {
			base.ProviderMeta[k] = v
		}
	}

	// Record provenance: list sources that contributed
	sources := base.ProviderMeta["sources"]
	if sources == "" {
		sources = base.Provider
	}
	if !strings.Contains(sources, extra.Provider) {
		sources += "," + extra.Provider
	}
	base.ProviderMeta["sources"] = sources

	return base
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
