package radio

import (
	"context"
	"testing"

	"swar/internal/catalog"
	"swar/internal/recommendations"
	"swar/internal/testkit"
)

func manager(t *testing.T) *Manager {
	cat, _ := testkit.Catalog(t, testkit.Options{})
	return NewManager(cat, recommendations.NewService(cat, nil), nil, 1<<62)
}

func TestSongRadioIsLabelledAndAvoidsRepeats(t *testing.T) {
	m := manager(t)
	snap, err := m.Start(context.Background(), "dev", KindSong, "saavn:r1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Generation != GenProviderStation || !snap.ProviderStation || snap.Label == "" {
		t.Fatalf("labels: %+v", snap)
	}
	seen := map[string]bool{"saavn:r1": true}
	all := snap.Tracks
	for i := 0; i < 3; i++ {
		s, err := m.Next(context.Background(), "dev", snap.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, s.Tracks...)
	}
	for _, tr := range all {
		if seen[tr.ID] {
			t.Fatalf("repeat %s in %v", tr.ID, all)
		}
		seen[tr.ID] = true
	}
}

func TestGeneratedRadioNotMislabelled(t *testing.T) {
	m := manager(t)
	for _, k := range []Kind{KindArtist, KindAlbum, KindGenre, KindDiscovery} {
		seed := map[Kind]string{KindArtist: "saavn:a1", KindAlbum: "saavn:al1", KindGenre: "lofi"}[k]
		snap, err := m.Start(context.Background(), "dev-"+string(k), k, seed, 3)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if snap.ProviderStation || snap.Generation == GenProviderStation {
			t.Errorf("%s must not claim to be a provider station", k)
		}
		if len(snap.Tracks) == 0 {
			t.Errorf("%s produced no tracks", k)
		}
	}
}

func TestValidationOwnershipAndFeedback(t *testing.T) {
	m := manager(t)
	if _, err := m.Start(context.Background(), "dev", KindGenre, "<script>", 3); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatalf("bad genre accepted: %v", err)
	}
	if _, err := m.Start(context.Background(), "dev", "weather", "", 3); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("unknown kind accepted")
	}
	snap, err := m.Start(context.Background(), "dev", KindSong, "saavn:r1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Next(context.Background(), "someone-else", snap.ID, 1); catalog.KindOf(err) != catalog.KindNotFound {
		t.Fatal("another owner must not access the session")
	}
	if _, err := m.Feedback("dev", snap.ID, "saavn:r2", "dislike"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Feedback("dev", snap.ID, "saavn:r2", "love"); catalog.KindOf(err) != catalog.KindInvalid {
		t.Fatal("bad action accepted")
	}
}

func TestPersonalizedFallsBackHonestly(t *testing.T) {
	m := manager(t)
	snap, err := m.Start(context.Background(), "dev", KindPersonalized, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Kind != KindDiscovery || snap.Generation != GenDiscovery {
		t.Fatalf("without history, personalized radio should say it fell back: %+v", snap)
	}
}

func TestSessionsPerOwnerBounded(t *testing.T) {
	m := manager(t)
	for i := 0; i < 5; i++ {
		if _, err := m.Start(context.Background(), "dev", KindSong, "saavn:r1", 1); err != nil {
			t.Fatal(err)
		}
	}
	if m.Len() != maxPerOwner {
		t.Fatalf("sessions = %d", m.Len())
	}
}
