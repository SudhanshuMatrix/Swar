package normalize

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// Noise patterns commonly found in music titles and videos.
	noiseSuffixRegex = regexp.MustCompile(`(?i)\s*(?:[\(\[]\s*(?:official\s*(?:video|audio|music\s*video|lyric\s*video|hd\s*video)?|lyric\s*video|audio|lyrics|hd|4k|full\s*(?:song|video|audio)|video\s*song)\s*[\)\]]|[-–]\s*full\s*(?:song|video|audio))\s*`)
	pipeSuffixRegex  = regexp.MustCompile(`(?i)\s*\|.*$`)
	whitespaceRegex  = regexp.MustCompile(`\s+`)
)

// CleanTitle cleans common promotional noise (e.g. "[Official Video]") from a display title,
// while preserving the original casing, Devanagari/regional characters, and meaningful subtitles.
func CleanTitle(title string) string {
	s := strings.TrimSpace(title)
	s = noiseSuffixRegex.ReplaceAllString(s, "")
	s = pipeSuffixRegex.ReplaceAllString(s, "")
	s = whitespaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// FoldForComparison normalizes a string for deduplication and matching:
// - Converts to lowercase.
// - Collapses whitespace.
// - Removes punctuation except within Devanagari and regional scripts.
// - Preserves Devanagari, Gurmukhi, Bengali, Tamil, Telugu, Kannada, Gujarati, Malayalam text.
func FoldForComparison(s string) string {
	s = CleanTitle(s)
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || isIndianScript(r) {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteRune(' ')
		}
	}

	result := whitespaceRegex.ReplaceAllString(b.String(), " ")
	return strings.TrimSpace(result)
}

// isIndianScript checks if a rune belongs to major Indian language Unicode blocks.
func isIndianScript(r rune) bool {
	return (r >= 0x0900 && r <= 0x097F) || // Devanagari (Hindi, Marathi, Sanskrit, Nepali)
		(r >= 0x0980 && r <= 0x09FF) || // Bengali / Assamese
		(r >= 0x0A00 && r <= 0x0A7F) || // Gurmukhi (Punjabi)
		(r >= 0x0A80 && r <= 0x0AFF) || // Gujarati
		(r >= 0x0B00 && r <= 0x0B7F) || // Oriya
		(r >= 0x0B80 && r <= 0x0BFF) || // Tamil
		(r >= 0x0C00 && r <= 0x0C7F) || // Telugu
		(r >= 0x0C80 && r <= 0x0CFF) || // Kannada
		(r >= 0x0D00 && r <= 0x0D7F) // Malayalam
}
