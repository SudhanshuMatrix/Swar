package playback

import (
	"sync"
	"testing"
)

func ids(q *Queue) []string { return q.TrackIDs() }

func cur(q *Queue) string {
	e, _ := q.CurrentEntry()
	return e.TrackID
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestQueueBasics(t *testing.T) {
	q := NewQueue()
	if _, ok := q.CurrentEntry(); ok {
		t.Fatal("empty queue has no current")
	}
	_ = q.Replace([]string{"a", "b", "c"}, 1, "", 10)
	if cur(q) != "b" {
		t.Fatalf("current = %s", cur(q))
	}
	_, _ = q.Add([]string{"d"}, "", 10)
	_, _ = q.PlayNext("x", "", 10)
	if !eq(ids(q), []string{"a", "b", "x", "c", "d"}) {
		t.Fatalf("order = %v", ids(q))
	}
	if up := q.Upcoming(2); len(up) != 2 || up[0].TrackID != "x" || up[1].TrackID != "c" {
		t.Fatalf("upcoming = %+v", up)
	}
	if !q.Next() || cur(q) != "x" {
		t.Fatal("next")
	}
	if !q.Previous() || cur(q) != "b" {
		t.Fatal("previous")
	}
}

func TestRemoveKeepsCurrentSensible(t *testing.T) {
	q := NewQueue()
	_ = q.Replace([]string{"a", "b", "c"}, 1, "", 10)
	e0 := q.Entries[0].EntryID
	_ = q.Remove(e0)
	if cur(q) != "b" {
		t.Fatalf("removing before current must keep it: %s", cur(q))
	}
	eb := q.Entries[q.Order[q.Current]].EntryID
	_ = q.Remove(eb)
	if cur(q) != "c" {
		t.Fatalf("removing current should advance: %s", cur(q))
	}
	_ = q.Remove(q.Entries[0].EntryID)
	if q.Current != -1 || len(q.Entries) != 0 {
		t.Fatal("queue should be empty")
	}
	if err := q.Remove("nope"); err != ErrEntryNotFound {
		t.Fatal("want not found")
	}
}

func TestMoveAndJump(t *testing.T) {
	q := NewQueue()
	_ = q.Replace([]string{"a", "b", "c", "d"}, 0, "", 10)
	d := q.Entries[3].EntryID
	if err := q.Move(d, 1); err != nil {
		t.Fatal(err)
	}
	if !eq(ids(q), []string{"a", "d", "b", "c"}) || cur(q) != "a" {
		t.Fatalf("after move: %v cur=%s", ids(q), cur(q))
	}
	a := q.Entries[0].EntryID
	_ = q.Move(a, 3)
	if !eq(ids(q), []string{"d", "b", "c", "a"}) || cur(q) != "a" || q.Current != 3 {
		t.Fatalf("moving current: %v cur=%s pos=%d", ids(q), cur(q), q.Current)
	}
	if err := q.Move(a, 9); err != ErrOutOfRange {
		t.Fatal("want out of range")
	}
	_ = q.Jump(q.Entries[1].EntryID)
	if cur(q) != "b" {
		t.Fatal("jump")
	}
}

func TestRepeatAndEnd(t *testing.T) {
	q := NewQueue()
	_ = q.Replace([]string{"a", "b"}, 1, "", 10)
	if q.Next() {
		t.Fatal("end of queue without repeat")
	}
	q.SetRepeat(RepeatAll)
	if !q.Next() || cur(q) != "a" {
		t.Fatal("repeat all wraps")
	}
	if !q.Previous() || cur(q) != "b" {
		t.Fatal("repeat all wraps backwards")
	}
	if up := q.Upcoming(5); len(up) != 1 || up[0].TrackID != "a" {
		t.Fatalf("upcoming with wrap = %+v", up)
	}
}

func TestShuffleKeepsCurrentAndRestores(t *testing.T) {
	q := NewQueue()
	list := []string{"a", "b", "c", "d", "e", "f", "g"}
	_ = q.Replace(list, 3, "", 10)
	q.SetShuffle(true)
	if cur(q) != "d" || q.Current != 0 {
		t.Fatalf("shuffle must keep the current track first: %s at %d", cur(q), q.Current)
	}
	seen := map[string]bool{}
	for _, id := range ids(q) {
		seen[id] = true
	}
	if len(seen) != len(list) {
		t.Fatal("shuffle lost tracks")
	}
	q.SetShuffle(false)
	if !eq(ids(q), list) || cur(q) != "d" {
		t.Fatalf("unshuffle: %v cur=%s", ids(q), cur(q))
	}
}

func TestLimitsAndRevision(t *testing.T) {
	q := NewQueue()
	if err := q.Replace([]string{"a", "b", "c"}, 0, "", 2); err != ErrQueueFull {
		t.Fatal("limit on replace")
	}
	_ = q.Replace([]string{"a"}, 0, "", 2)
	r := q.Revision
	_, _ = q.Add([]string{"b"}, "", 2)
	if _, err := q.Add([]string{"c"}, "", 2); err != ErrQueueFull {
		t.Fatal("limit on add")
	}
	if q.Revision != r+1 {
		t.Fatal("revision must bump on change only")
	}
}

func TestValidateRepairs(t *testing.T) {
	q := &Queue{Entries: []Entry{{EntryID: "1", TrackID: "a"}}, Order: []int{}, Current: 9}
	q.Validate()
	if len(q.Order) != 1 || q.Current != 0 || q.Repeat != RepeatOff {
		t.Fatalf("%+v", q)
	}
}

func TestCloneIsDeep(t *testing.T) {
	q := NewQueue()
	_ = q.Replace([]string{"a"}, 0, "", 5)
	c := q.Clone()
	_, _ = c.Add([]string{"b"}, "", 5)
	if len(q.Entries) != 1 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentUseWithLock(t *testing.T) {
	// Queue is not goroutine-safe by itself; owners guard it. This checks
	// the invariants hold under a mutex with many writers.
	q := NewQueue()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			_, _ = q.Add([]string{"t"}, "", 1000)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(q.Entries) != 50 || len(q.Order) != 50 {
		t.Fatalf("entries=%d order=%d", len(q.Entries), len(q.Order))
	}
}
