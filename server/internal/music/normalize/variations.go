package normalize

import (
	"regexp"
	"strings"
)

// VariationTags holds boolean flags for specific recording types.
type VariationTags struct {
	IsRemix        bool
	IsLive         bool
	IsAcoustic     bool
	IsCover        bool
	IsInstrumental bool
	IsLofi         bool
	IsReprise      bool
}

var (
	remixRegex        = regexp.MustCompile(`(?i)\b(remix|re-mix|mix|club\s*mix|extended\s*mix|dj\s*(?:mix)?|dance\s*mix)\b`)
	liveRegex         = regexp.MustCompile(`(?i)\b(live|concert|tour|unplugged\s*live|in\s*concert)\b`)
	acousticRegex     = regexp.MustCompile(`(?i)\b(acoustic|unplugged|piano\s*(?:version)?|stripped)\b`)
	coverRegex        = regexp.MustCompile(`(?i)\b(cover|tribute|rendition)\b`)
	instrumentalRegex = regexp.MustCompile(`(?i)\b(instrumental|karaoke|backing\s*track|minus\s*one)\b`)
	lofiRegex         = regexp.MustCompile(`(?i)\b(lo-?fi|slowed(?:\s*(?:and|\+)?\s*reverb)?|chill\s*mix)\b`)
	repriseRegex      = regexp.MustCompile(`(?i)\b(reprise|female\s*version|male\s*version|sad\s*version|part\s*[2-9]|duet\s*version)\b`)
)

// ExtractVariationTags parses a title and returns its variation flags.
func ExtractVariationTags(title string) VariationTags {
	lower := strings.ToLower(title)
	return VariationTags{
		IsRemix:        remixRegex.MatchString(lower),
		IsLive:         liveRegex.MatchString(lower),
		IsAcoustic:     acousticRegex.MatchString(lower),
		IsCover:        coverRegex.MatchString(lower),
		IsInstrumental: instrumentalRegex.MatchString(lower),
		IsLofi:         lofiRegex.MatchString(lower),
		IsReprise:      repriseRegex.MatchString(lower),
	}
}

// AreCompatibleVersions checks if two titles represent the same recording type.
// If one title is a remix, live performance, acoustic rendition, instrumental,
// cover, or lofi mix while the other is not, they are NOT compatible and must NOT be merged.
func AreCompatibleVersions(titleA, titleB string) bool {
	tagsA := ExtractVariationTags(titleA)
	tagsB := ExtractVariationTags(titleB)

	if tagsA.IsRemix != tagsB.IsRemix {
		return false
	}
	if tagsA.IsLive != tagsB.IsLive {
		return false
	}
	if tagsA.IsAcoustic != tagsB.IsAcoustic {
		return false
	}
	if tagsA.IsCover != tagsB.IsCover {
		return false
	}
	if tagsA.IsInstrumental != tagsB.IsInstrumental {
		return false
	}
	if tagsA.IsLofi != tagsB.IsLofi {
		return false
	}
	if tagsA.IsReprise != tagsB.IsReprise {
		return false
	}
	return true
}

// BaseTitleWithoutVariation returns the title with variation keywords stripped out,
// useful for grouping alternate versions together while keeping their distinct identities.
func BaseTitleWithoutVariation(title string) string {
	s := CleanTitle(title)
	s = remixRegex.ReplaceAllString(s, "")
	s = liveRegex.ReplaceAllString(s, "")
	s = acousticRegex.ReplaceAllString(s, "")
	s = coverRegex.ReplaceAllString(s, "")
	s = instrumentalRegex.ReplaceAllString(s, "")
	s = lofiRegex.ReplaceAllString(s, "")
	s = repriseRegex.ReplaceAllString(s, "")
	return FoldForComparison(s)
}
