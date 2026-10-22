package normalize

import (
	"math"
	"sort"
	"strings"
)

// JaroSimilarity calculates the Jaro distance between two strings (0.0 to 1.0).
func JaroSimilarity(s1, s2 string) float64 {
	r1 := []rune(s1)
	r2 := []rune(s2)

	l1 := len(r1)
	l2 := len(r2)

	if l1 == 0 && l2 == 0 {
		return 1.0
	}
	if l1 == 0 || l2 == 0 {
		return 0.0
	}

	matchDistance := int(math.Floor(float64(max(l1, l2))/2.0)) - 1
	if matchDistance < 0 {
		matchDistance = 0
	}

	s1Matches := make([]bool, l1)
	s2Matches := make([]bool, l2)

	matches := 0
	for i := 0; i < l1; i++ {
		start := max(0, i-matchDistance)
		end := min(i+matchDistance+1, l2)

		for j := start; j < end; j++ {
			if s2Matches[j] || r1[i] != r2[j] {
				continue
			}
			s1Matches[i] = true
			s2Matches[j] = true
			matches++
			break
		}
	}

	if matches == 0 {
		return 0.0
	}

	transpositions := 0
	k := 0
	for i := 0; i < l1; i++ {
		if !s1Matches[i] {
			continue
		}
		for !s2Matches[k] {
			k++
		}
		if r1[i] != r2[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	return (m/float64(l1) + m/float64(l2) + (m-float64(transpositions)/2.0)/m) / 3.0
}

// JaroWinkler calculates the Jaro-Winkler similarity (0.0 to 1.0),
// giving a boost to common prefixes up to 4 characters.
func JaroWinkler(s1, s2 string) float64 {
	jaro := JaroSimilarity(s1, s2)
	if jaro < 0.7 {
		return jaro
	}

	r1 := []rune(s1)
	r2 := []rune(s2)

	prefixLen := 0
	maxPrefix := min(4, min(len(r1), len(r2)))
	for i := 0; i < maxPrefix; i++ {
		if r1[i] == r2[i] {
			prefixLen++
		} else {
			break
		}
	}

	// Standard scaling factor p = 0.1
	return jaro + (float64(prefixLen) * 0.1 * (1.0 - jaro))
}

// TokenSetRatio splits two strings into unique tokens, finds the intersection
// and remaining differences, and computes similarity. This handles swapped words
// (e.g., "Arijit Singh - Kesariya" vs "Kesariya - Arijit Singh") effectively.
func TokenSetRatio(s1, s2 string) float64 {
	tokens1 := uniqueTokens(s1)
	tokens2 := uniqueTokens(s2)

	if len(tokens1) == 0 && len(tokens2) == 0 {
		return 1.0
	}
	if len(tokens1) == 0 || len(tokens2) == 0 {
		return 0.0
	}

	// Calculate intersection
	interMap := make(map[string]bool)
	for _, t := range tokens1 {
		for _, u := range tokens2 {
			if t == u {
				interMap[t] = true
				break
			}
		}
	}

	intersection := make([]string, 0, len(interMap))
	for t := range interMap {
		intersection = append(intersection, t)
	}
	sort.Strings(intersection)

	interStr := strings.Join(intersection, " ")
	t1Str := strings.Join(tokens1, " ")
	t2Str := strings.Join(tokens2, " ")

	if len(intersection) == 0 {
		return JaroWinkler(t1Str, t2Str)
	}

	score1 := JaroWinkler(interStr, t1Str)
	score2 := JaroWinkler(interStr, t2Str)
	score3 := JaroWinkler(t1Str, t2Str)

	return max(score1, max(score2, score3))
}

func uniqueTokens(s string) []string {
	fields := strings.Fields(FoldForComparison(s))
	seen := make(map[string]bool, len(fields))
	var out []string
	for _, f := range fields {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// PhoneticFold normalizes common Indian-English transliteration phonetic variants,
// such as "ee"/"i", "oo"/"u", "aa"/"a", "sh"/"s", "v"/"w", "z"/"j".
func PhoneticFold(s string) string {
	folded := FoldForComparison(s)
	replacements := []struct {
		from, to string
	}{
		{"iya", "ia"},
		{"ya", "ia"},
		{"aa", "a"},
		{"ee", "i"},
		{"oo", "u"},
		{"sh", "s"},
		{"ph", "f"},
		{"th", "t"},
		{"dh", "d"},
		{"kh", "k"},
		{"gh", "g"},
		{"bh", "b"},
		{"ch", "c"},
		{"zh", "z"},
		{"w", "v"},
		{"z", "j"},
	}

	for _, r := range replacements {
		folded = strings.ReplaceAll(folded, r.from, r.to)
	}
	return folded
}

// BestMatchScore returns the maximum similarity score across raw Jaro-Winkler,
// TokenSetRatio, and Phonetically folded matching.
func BestMatchScore(s1, s2 string) float64 {
	f1 := FoldForComparison(s1)
	f2 := FoldForComparison(s2)

	if f1 == f2 {
		return 1.0
	}

	jw := JaroWinkler(f1, f2)
	tsr := TokenSetRatio(f1, f2)
	phoneticJW := JaroWinkler(PhoneticFold(s1), PhoneticFold(s2))

	return max(jw, max(tsr, phoneticJW))
}
