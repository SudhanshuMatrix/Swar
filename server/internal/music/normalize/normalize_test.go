package normalize

import (
	"testing"
)

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Kesariya (From \"Brahmastra\") [Official Video]", "Kesariya (From \"Brahmastra\")"},
		{"Tum Hi Ho | Aashiqui 2 | Official Music Video", "Tum Hi Ho"},
		{"Apna Bana Le [Lyric Video]", "Apna Bana Le"},
		{"Raataan Lambiyan - Full Song", "Raataan Lambiyan"},
		{"Chaleya (Hindi) [HD]", "Chaleya (Hindi)"},
		{"Ordinary Clean Title", "Ordinary Clean Title"},
	}

	for _, tt := range tests {
		got := CleanTitle(tt.input)
		if got != tt.expected {
			t.Errorf("CleanTitle(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFoldForComparisonPreservesIndianScripts(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Kesariya", "kesariya"},
		{"केसरिया", "केसरिया"},
		{"रातां लम्बियां", "रातां लम्बियां"},
		{"ਤਾਰੇ ਗਿਣ ਗਿਣ", "ਤਾਰੇ ਗਿਣ ਗਿਣ"},   // Gurmukhi
		{"கண்ணே கலைமானே", "கண்ணே கலைமானே"}, // Tamil
		{"Special! Chars & Punctuation???", "special chars punctuation"},
	}

	for _, tt := range tests {
		got := FoldForComparison(tt.input)
		if got != tt.expected {
			t.Errorf("FoldForComparison(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestVariationsCompatibility(t *testing.T) {
	// Incompatible pairs: must NOT be merged
	incompatible := [][2]string{
		{"Kesariya", "Kesariya (Remix)"},
		{"Tum Hi Ho (Live in Concert)", "Tum Hi Ho"},
		{"Kabira", "Kabira (Acoustic Version)"},
		{"Channa Mereya", "Channa Mereya (Cover)"},
		{"Kal Ho Naa Ho", "Kal Ho Naa Ho (Instrumental)"},
		{"Pasoori", "Pasoori (Lo-fi Chill Mix)"},
		{"Tujh Mein Rab Dikhta Hai", "Tujh Mein Rab Dikhta Hai (Female Version)"},
	}

	for _, pair := range incompatible {
		if AreCompatibleVersions(pair[0], pair[1]) {
			t.Errorf("expected %q and %q to be incompatible, but got compatible", pair[0], pair[1])
		}
	}

	// Compatible pairs: should be allowed to merge if matching
	compatible := [][2]string{
		{"Kesariya", "Kesariya"},
		{"Kesariya (Remix)", "Kesariya [Club Mix]"},
		{"Tum Hi Ho (Live)", "Tum Hi Ho [Live in Concert]"},
		{"Kabira (Acoustic)", "Kabira - Acoustic Version"},
	}

	for _, pair := range compatible {
		if !AreCompatibleVersions(pair[0], pair[1]) {
			t.Errorf("expected %q and %q to be compatible, but got incompatible", pair[0], pair[1])
		}
	}
}

func TestStringMatching(t *testing.T) {
	// Exact match
	if score := JaroWinkler("kesariya", "kesariya"); score != 1.0 {
		t.Errorf("expected 1.0 for exact match, got %f", score)
	}

	// Swapped token order should have high TokenSetRatio
	s1 := "Arijit Singh Kesariya"
	s2 := "Kesariya Arijit Singh"
	tsr := TokenSetRatio(s1, s2)
	if tsr < 0.95 {
		t.Errorf("expected high TokenSetRatio for word swap, got %f", tsr)
	}

	// Phonetic folding
	p1 := PhoneticFold("Kesariya")
	p2 := PhoneticFold("Kesaria")
	if p1 != p2 {
		t.Errorf("expected phonetic folding to match %q and %q, got %q and %q", "Kesariya", "Kesaria", p1, p2)
	}

	matchScore := BestMatchScore("Tum Hi Ho", "Tum Hee Ho")
	if matchScore < 0.95 {
		t.Errorf("expected high match score with phonetic match, got %f", matchScore)
	}
}
