package recommendations

import (
	"context"
	"testing"
	"time"

	"swar/internal/catalog"
	"swar/internal/testkit"
)

func tr(id, artistID, lang string) catalog.Track {
	return catalog.Track{ID: id, Language: lang, Artists: []catalog.ArtistRef{{ID: artistID, Name: artistID, Role: "primary"}}}
}

func TestRankingIsExplainable(t *testing.T) {
	seed := tr("seed", "A", "hindi")
	cands := []Candidate{
		{Track: tr("related", "B", "hindi"), Sources: []string{ReasonProviderRelated}, Rank: 0},
		{Track: tr("sameArtist", "A", "hindi"), Sources: []string{ReasonSameArtist}},
		{Track: tr("both", "A", "hindi"), Sources: []string{ReasonProviderRelated}, Rank: 1},
		{Track: tr("both", "A", "hindi"), Sources: []string{ReasonSameArtist}}, // merged
		{Track: tr("other", "C", "english"), Sources: []string{"misc"}},
	}
	out := Rank(&seed, cands, Signals{}, nil, time.Now())
	if len(out) != 4 || out[0].Track.ID != "both" {
		t.Fatalf("order: %+v", out)
	}
	if out[len(out)-1].Track.ID != "other" {
		t.Errorf("unrelated track should rank last: %+v", out)
	}
	has := func(r Ranked, reason string) bool {
		for _, x := range r.Reasons {
			if x == reason {
				return true
			}
		}
		return false
	}
	if !has(out[0], ReasonProviderRelated) || !has(out[0], ReasonSameArtist) || !has(out[0], ReasonSameLanguage) {
		t.Errorf("reasons: %v", out[0].Reasons)
	}
}

func TestDislikesExplicitAndExcludesAreDropped(t *testing.T) {
	seed := tr("seed", "A", "")
	ex := tr("explicit", "B", "")
	ex.Explicit = true
	cands := []Candidate{
		{Track: seed}, {Track: tr("disliked", "B", "")}, {Track: ex}, {Track: tr("inqueue", "B", "")}, {Track: tr("ok", "B", "")},
	}
	sig := Signals{Disliked: map[string]bool{"disliked": true}, BlockExplicit: true}
	out := Rank(&seed, cands, sig, map[string]bool{"inqueue": true}, time.Now())
	if len(out) != 1 || out[0].Track.ID != "ok" {
		t.Fatalf("got %+v", out)
	}
}

func TestRepetitionAndSkipPenalties(t *testing.T) {
	now := time.Now()
	seed := tr("seed", "A", "")
	cands := []Candidate{
		{Track: tr("justPlayed", "B", ""), Sources: []string{ReasonProviderRelated}, Rank: 0},
		{Track: tr("playedLastWeekish", "B", ""), Sources: []string{ReasonProviderRelated}, Rank: 1},
		{Track: tr("skipped", "B", ""), Sources: []string{ReasonProviderRelated}, Rank: 2},
		{Track: tr("fresh", "B", ""), Sources: []string{ReasonProviderRelated}, Rank: 3},
	}
	sig := Signals{
		RecentPlays: map[string]time.Time{"justPlayed": now.Add(-10 * time.Minute), "playedLastWeekish": now.Add(-6 * 24 * time.Hour)},
		Skips:       map[string]int{"skipped": 2},
	}
	out := Rank(&seed, cands, sig, nil, now)
	if out[0].Track.ID != "fresh" || out[len(out)-1].Track.ID != "justPlayed" {
		t.Fatalf("order: %v", ids(out))
	}
}

func TestPersonalSignalsBoost(t *testing.T) {
	seed := tr("seed", "A", "")
	cands := []Candidate{
		{Track: tr("x", "B", "tamil"), Sources: []string{"misc"}},
		{Track: tr("y", "LIKED", "punjabi"), Sources: []string{"misc"}},
	}
	sig := Signals{LikedArtists: map[string]int{"LIKED": 3}, Languages: map[string]bool{"punjabi": true}}
	out := Rank(&seed, cands, sig, nil, time.Now())
	if out[0].Track.ID != "y" {
		t.Fatalf("liked artist + preferred language should win: %v", ids(out))
	}
}

func TestArtistDiversity(t *testing.T) {
	in := []Ranked{{Track: tr("1", "A", "")}, {Track: tr("2", "A", "")}, {Track: tr("3", "A", "")}, {Track: tr("4", "B", "")}}
	out := ApplyArtistDiversity(in, 2)
	if got := ids(out); got[2] != "4" || len(got) != 4 {
		t.Fatalf("no more than two in a row: %v", got)
	}
}

func ids(rs []Ranked) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Track.ID)
	}
	return out
}

func TestServiceNextFromCatalog(t *testing.T) {
	cat, f := testkit.Catalog(t, testkit.Options{})
	svc := NewService(cat, nil)
	res, err := svc.Next(context.Background(), Request{SeedTrackID: "saavn:r1", Exclude: []string{"saavn:r3"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(res.Results)
	for _, id := range got {
		if id == "saavn:r1" || id == "saavn:r3" {
			t.Fatalf("seed/excluded leaked: %v", got)
		}
	}
	if len(got) == 0 || res.Degraded {
		t.Fatalf("results: %+v", res)
	}
	// A failing pool degrades instead of failing.
	f.FailNext("webradio.createEntityStation", 500)
	res, err = svc.Next(context.Background(), Request{SeedTrackID: "saavn:x1", Limit: 5})
	if err != nil || !res.Degraded {
		t.Fatalf("want degraded result, got %+v %v", res, err)
	}
}
