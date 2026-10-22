package jam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"swar/internal/catalog"
)

type fakeTracks struct{}

func (fakeTracks) Tracks(_ context.Context, ids []string) ([]catalog.Track, error) {
	out := []catalog.Track{}
	for _, id := range dedupeIDs(ids) {
		if id == "saavn:missing" {
			continue
		}
		out = append(out, catalog.Track{ID: id, Title: "T " + id})
	}
	return out, nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newManager(t *testing.T) (*Manager, *clock) {
	t.Helper()
	m := NewManager(Options{MaxParticipants: 3, MaxQueue: 5, IdleTTL: 30 * time.Minute, MaxLifetime: 12 * time.Hour}, fakeTracks{}, nil)
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	m.SetClock(c.now)
	return m, c
}

func wantErr(t *testing.T, err error, want *Error) {
	t.Helper()
	var je *Error
	if !errors.As(err, &je) || je.Code != want.Code {
		t.Fatalf("err = %v, want %s", err, want.Code)
	}
}

func cmd(typ string) Command { return Command{Type: typ} }

func TestCreateAndJoin(t *testing.T) {
	m, _ := newManager(t)
	host, err := m.Create("dev_host", "", "Asha", Settings{AllowParticipantAdd: true})
	if err != nil {
		t.Fatal(err)
	}
	if host.Role != RoleAdmin || len(host.Code) != 6 || host.Name != "Asha's Jam" {
		t.Fatalf("host snapshot: %+v", host)
	}
	// Codes are case- and separator-insensitive.
	lower := []byte(host.Code)
	for i := range lower {
		if lower[i] >= 'A' && lower[i] <= 'Z' {
			lower[i] += 'a' - 'A'
		}
	}
	code := string(lower[:3]) + "-" + string(lower[3:])
	g, err := m.Join("dev_guest", code, "Ravi")
	if err != nil {
		t.Fatal(err)
	}
	if g.Role != RoleParticipant || len(g.Participants) != 2 || g.Participants[0].Role != RoleAdmin {
		t.Fatalf("guest snapshot: %+v", g)
	}
	// Rejoining is idempotent.
	g2, err := m.Join("dev_guest", host.Code, "Ravi")
	if err != nil || g2.You != g.You || len(g2.Participants) != 2 {
		t.Fatalf("rejoin: %v %+v", err, g2)
	}
	if g.Removed != nil {
		t.Fatal("participants must not see the removed list")
	}
}

func TestJoinErrors(t *testing.T) {
	m, c := newManager(t)
	for _, code := range []string{"", "ABC", "ZZZZZZ", "OOOOOO", "ABCDEFG"} {
		if _, err := m.Join("dev_x", code, "x"); err == nil {
			t.Fatalf("code %q accepted", code)
		} else {
			wantErr(t, err, ErrInvalidCode)
		}
	}
	h, _ := m.Create("dev_h", "Party", "H", Settings{})
	m.Join("dev_a", h.Code, "A")
	m.Join("dev_b", h.Code, "B")
	_, err := m.Join("dev_c", h.Code, "C")
	wantErr(t, err, ErrFull)

	// Ended by the admin.
	if _, err := m.Execute(context.Background(), "dev_h", h.ID, cmd("end")); err != nil {
		t.Fatal(err)
	}
	_, err = m.Join("dev_d", h.Code, "D")
	wantErr(t, err, ErrEnded)
	_, err = m.Get("dev_a", h.ID)
	wantErr(t, err, ErrEnded)

	// Expired after idling.
	h2, _ := m.Create("dev_h2", "", "H2", Settings{})
	c.add(31 * time.Minute)
	m.GC()
	_, err = m.Join("dev_e", h2.Code, "E")
	wantErr(t, err, ErrExpired)

	// Tombstones are forgotten after an hour; the code is then invalid.
	c.add(2 * time.Hour)
	m.GC()
	_, err = m.Join("dev_e", h2.Code, "E")
	wantErr(t, err, ErrInvalidCode)
	if m.ActiveCount() != 0 {
		t.Fatalf("active = %d", m.ActiveCount())
	}
}

func TestJoinRateLimit(t *testing.T) {
	m, c := newManager(t)
	for i := 0; i < 10; i++ {
		_, err := m.Join("dev_brute", fmt.Sprintf("AAAA%c%c", codeAlphabet[i], codeAlphabet[i+1]), "x")
		wantErr(t, err, ErrInvalidCode)
	}
	_, err := m.Join("dev_brute", "AAAAAA", "x")
	wantErr(t, err, ErrRateLimited)
	// Other devices are unaffected; the window resets.
	if _, err := m.Join("dev_other", "AAAAAA", "x"); errors.Is(err, ErrRateLimited) {
		t.Fatal("limit leaked across devices")
	}
	c.add(time.Minute + time.Second)
	_, err = m.Join("dev_brute", "AAAAAA", "x")
	wantErr(t, err, ErrInvalidCode)
}

func TestAdminOnlyAndPermissions(t *testing.T) {
	m, _ := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{})
	m.Join("dev_p", h.Code, "P")
	for _, typ := range []string{"play", "pause", "next", "previous", "remove_participant", "update_settings", "end"} {
		_, err := m.Execute(ctx, "dev_p", h.ID, cmd(typ))
		wantErr(t, err, ErrAdminOnly)
	}
	// Participant add is off by default.
	_, err := m.Execute(ctx, "dev_p", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:s1"}})
	wantErr(t, err, ErrNotAllowed)
	// Non-members cannot act or trigger lookups.
	_, err = m.Execute(ctx, "dev_stranger", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:s1"}})
	wantErr(t, err, ErrNotMember)
	// Unknown tracks are rejected.
	_, err = m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:missing"}})
	wantErr(t, err, ErrUnknownTrack)
	_, err = m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"not an id!"}})
	wantErr(t, err, ErrBadCommand)

	// Admin enables adding; the participant can then add and remove their own entry.
	allow := Settings{AllowParticipantAdd: true}
	if _, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "update_settings", Settings: &allow}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:h1"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := m.Execute(ctx, "dev_p", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:p1"}})
	if err != nil {
		t.Fatal(err)
	}
	hostEntry, mine := snap.Queue.Entries[0].EntryID, snap.Queue.Entries[1].EntryID
	_, err = m.Execute(ctx, "dev_p", h.ID, Command{Type: "queue_remove", EntryID: hostEntry})
	wantErr(t, err, ErrAdminOnly)
	if _, err := m.Execute(ctx, "dev_p", h.ID, Command{Type: "queue_remove", EntryID: mine}); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackStateAnchoredToServerTime(t *testing.T) {
	m, c := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{})
	_, err := m.Execute(ctx, "dev_h", h.ID, cmd("play"))
	wantErr(t, err, ErrBadCommand) // empty queue
	snap, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:a", "saavn:b"}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.NowPlaying.TrackID != "saavn:a" || snap.NowPlaying.Playing {
		t.Fatalf("now playing: %+v", snap.NowPlaying)
	}
	// Play is scheduled a moment ahead so every device starts together.
	playAt := c.now().UnixMilli() + resumeLeadMs
	snap, _ = m.Execute(ctx, "dev_h", h.ID, cmd("play"))
	if snap.NowPlaying.UpdatedAt != playAt || snap.NowPlaying.PositionMs != 0 {
		t.Fatalf("play not scheduled ahead: %+v", snap.NowPlaying)
	}
	c.add(10 * time.Second)
	m.Join("dev_late", h.Code, "Late")
	late, _ := m.Get("dev_late", h.ID)
	if late.NowPlaying.PositionMs != 10_000-resumeLeadMs {
		t.Fatalf("late joiner position = %d", late.NowPlaying.PositionMs)
	}
	snap, _ = m.Execute(ctx, "dev_h", h.ID, cmd("pause"))
	c.add(5 * time.Second)
	if got, _ := m.Get("dev_h", h.ID); got.NowPlaying.PositionMs != 10_000-resumeLeadMs || got.NowPlaying.Playing {
		t.Fatalf("paused position drifted: %+v", got.NowPlaying)
	}
	pos := int64(42_000)
	snap, _ = m.Execute(ctx, "dev_h", h.ID, Command{Type: "seek", PositionMs: &pos})
	if snap.NowPlaying.PositionMs != pos {
		t.Fatalf("seek: %+v", snap.NowPlaying)
	}
	// track_ended for a stale entry is ignored; for the current one it advances.
	snap, _ = m.Execute(ctx, "dev_h", h.ID, Command{Type: "track_ended", EntryID: "nope"})
	if snap.NowPlaying.TrackID != "saavn:a" {
		t.Fatal("stale track_ended advanced the queue")
	}
	snap, _ = m.Execute(ctx, "dev_h", h.ID, Command{Type: "track_ended", EntryID: snap.NowPlaying.EntryID})
	if snap.NowPlaying.TrackID != "saavn:b" || snap.NowPlaying.PositionMs != 0 {
		t.Fatalf("after track_ended: %+v", snap.NowPlaying)
	}
}

func TestStaleRevisionRejected(t *testing.T) {
	m, _ := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{})
	snap, _ := m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:a", "saavn:b"}})
	old := snap.Rev
	m.Execute(ctx, "dev_h", h.ID, cmd("play"))
	_, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "next", Rev: &old})
	wantErr(t, err, ErrStale)
	// Additive commands are not revision-checked.
	if _, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", Rev: &old, TrackIDs: []string{"saavn:c"}}); err != nil {
		t.Fatal(err)
	}
}

func TestRemovalBlocksUntilRejoinAllowed(t *testing.T) {
	m, _ := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{AllowParticipantAdd: true})
	p, _ := m.Join("dev_p", h.Code, "P")
	conn, _, err := m.Connect("dev_p", h.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Execute(ctx, "dev_h", h.ID, Command{Type: "remove_participant", ParticipantID: h.You})
	wantErr(t, err, ErrBadCommand) // admin cannot be removed
	hs, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "remove_participant", ParticipantID: p.You})
	if err != nil {
		t.Fatal(err)
	}
	if len(hs.Removed) != 1 || len(hs.Participants) != 1 {
		t.Fatalf("admin view after removal: %+v", hs)
	}
	// The live connection is told and dropped.
	select {
	case <-conn.Done():
	case <-time.After(time.Second):
		t.Fatal("removed connection still open")
	}
	if conn.Reason() != "removed" {
		t.Fatalf("reason = %q", conn.Reason())
	}
	var sawRemoved bool
	for len(conn.Send()) > 0 {
		var msg struct{ Type string }
		json.Unmarshal(<-conn.Send(), &msg)
		sawRemoved = sawRemoved || msg.Type == "removed"
	}
	if !sawRemoved {
		t.Fatal("no removed message")
	}
	// Stale token / reconnect / commands / rejoin are all refused.
	_, err = m.Execute(ctx, "dev_p", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:x"}})
	wantErr(t, err, ErrRemoved)
	_, _, err = m.Connect("dev_p", h.ID)
	wantErr(t, err, ErrRemoved)
	_, err = m.Join("dev_p", h.Code, "P")
	wantErr(t, err, ErrRemoved)

	if _, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "allow_rejoin", ParticipantID: p.You}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Join("dev_p", h.Code, "P"); err != nil {
		t.Fatalf("rejoin after allow: %v", err)
	}
}

func TestHostLeavingEndsJam(t *testing.T) {
	m, _ := newManager(t)
	h, _ := m.Create("dev_h", "", "H", Settings{})
	m.Join("dev_p", h.Code, "P")
	conn, _, _ := m.Connect("dev_p", h.ID)
	if err := m.Leave("dev_h", h.ID); err != nil {
		t.Fatal(err)
	}
	<-conn.Done()
	if conn.Reason() != "ended" {
		t.Fatalf("reason = %q", conn.Reason())
	}
	_, err := m.Get("dev_p", h.ID)
	wantErr(t, err, ErrEnded)
}

func TestReconnectResyncsState(t *testing.T) {
	m, _ := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{})
	m.Join("dev_p", h.Code, "P")
	c1, _, _ := m.Connect("dev_p", h.ID)
	m.Disconnect(h.ID, c1)
	// Changes made while disconnected...
	m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:a"}})
	// ...arrive in the initial state message on reconnect.
	c2, initial, err := m.Connect("dev_p", h.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect(h.ID, c2)
	var msg struct {
		Type string
		Jam  Snapshot
	}
	if err := json.Unmarshal(initial, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "state" || len(msg.Jam.Queue.Entries) != 1 || msg.Jam.NowPlaying.TrackID != "saavn:a" {
		t.Fatalf("resync state: %s", initial)
	}
	// Broadcasts reach the live connection.
	m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:b"}})
	select {
	case b := <-c2.Send():
		json.Unmarshal(b, &msg)
		if len(msg.Jam.Queue.Entries) != 2 {
			t.Fatalf("broadcast: %s", b)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
	if b, err := m.SyncMessage("dev_p", h.ID); err != nil || !json.Valid(b) {
		t.Fatalf("sync: %v", err)
	}
}

func TestConcurrentQueueAdds(t *testing.T) {
	m := NewManager(Options{MaxParticipants: 20, MaxQueue: 200, IdleTTL: time.Hour, MaxLifetime: time.Hour}, fakeTracks{}, nil)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{AllowParticipantAdd: true})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		dev := fmt.Sprintf("dev_%d", i)
		if _, err := m.Join(dev, h.Code, dev); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if _, err := m.Execute(ctx, dev, h.ID, Command{Type: "queue_add", TrackIDs: []string{fmt.Sprintf("saavn:%s_%d", dev, j)}}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	snap, _ := m.Get("dev_h", h.ID)
	if len(snap.Queue.Entries) != 50 {
		t.Fatalf("entries = %d, want 50", len(snap.Queue.Entries))
	}
	seen := map[string]bool{}
	for _, e := range snap.Queue.Entries {
		if seen[e.EntryID] {
			t.Fatal("duplicate entry id")
		}
		seen[e.EntryID] = true
	}
}

func TestQueueLimitAndVotes(t *testing.T) {
	m, _ := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{AllowParticipantAdd: true, AllowVoting: true})
	m.Join("dev_p", h.Code, "P")
	snap, err := m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:a", "saavn:b", "saavn:c"}})
	if err != nil {
		t.Fatal(err)
	}
	cEntry := snap.Queue.Entries[2].EntryID
	snap, err = m.Execute(ctx, "dev_p", h.ID, Command{Type: "vote", EntryID: cEntry})
	if err != nil {
		t.Fatal(err)
	}
	// The current track stays; the voted entry moves to the front of upcoming.
	if snap.Queue.Entries[0].Track.ID != "saavn:a" || snap.Queue.Entries[1].EntryID != cEntry || !snap.Queue.Entries[1].Voted || snap.Queue.Entries[1].Votes != 1 {
		t.Fatalf("after vote: %+v", snap.Queue.Entries)
	}
	_, err = m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:d", "saavn:e", "saavn:f"}})
	wantErr(t, err, ErrQueueFull)
}

func TestNextSongStartsTogetherAfterALead(t *testing.T) {
	m, c := newManager(t)
	ctx := context.Background()
	h, _ := m.Create("dev_h", "", "H", Settings{})
	m.Execute(ctx, "dev_h", h.ID, Command{Type: "queue_add", TrackIDs: []string{"saavn:a", "saavn:b"}})
	m.Execute(ctx, "dev_h", h.ID, cmd("play"))
	c.add(30 * time.Second)
	startAt := c.now().UnixMilli() + trackLeadMs
	snap, _ := m.Execute(ctx, "dev_h", h.ID, cmd("next"))
	if snap.NowPlaying.TrackID != "saavn:b" || snap.NowPlaying.UpdatedAt != startAt || snap.NowPlaying.PositionMs != 0 {
		t.Fatalf("next song not scheduled ahead: %+v", snap.NowPlaying)
	}
	c.add(time.Second) // still waiting for the shared start
	if got, _ := m.Get("dev_h", h.ID); got.NowPlaying.PositionMs != 0 || got.NowPlaying.UpdatedAt != startAt {
		t.Fatalf("waiting position = %+v", got.NowPlaying)
	}
}
