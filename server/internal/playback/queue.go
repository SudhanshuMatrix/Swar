// Package playback holds the queue model shared by personal queues and
// Jam sessions, and resolves short-lived playback descriptors. The server
// only ever hands out references to authorized sources; it never reads,
// proxies or stores media bytes.
package playback

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	mrand "math/rand/v2"
)

type RepeatMode string

const (
	RepeatOff RepeatMode = "off"
	RepeatAll RepeatMode = "all"
	RepeatOne RepeatMode = "one"
)

func ParseRepeat(s string) (RepeatMode, bool) {
	switch RepeatMode(s) {
	case RepeatOff, RepeatAll, RepeatOne:
		return RepeatMode(s), true
	}
	return "", false
}

// Entry is one queue slot. EntryID distinguishes duplicates of the same
// track so remove/move operations are unambiguous.
type Entry struct {
	EntryID string `json:"entryId"`
	TrackID string `json:"trackId"`
	AddedBy string `json:"addedBy,omitempty"`
}

// Queue is a play queue with shuffle and repeat. Order holds indexes into
// Entries giving the effective play order; it is the identity when shuffle
// is off. Current is a position in Order (-1 when empty).
type Queue struct {
	Entries  []Entry    `json:"entries"`
	Order    []int      `json:"order"`
	Current  int        `json:"current"`
	Shuffle  bool       `json:"shuffle"`
	Repeat   RepeatMode `json:"repeat"`
	Revision int64      `json:"revision"`
}

var (
	ErrEntryNotFound = errors.New("queue entry not found")
	ErrQueueFull     = errors.New("queue is full")
	ErrOutOfRange    = errors.New("position out of range")
)

func NewQueue() *Queue {
	return &Queue{Entries: []Entry{}, Order: []int{}, Current: -1, Repeat: RepeatOff}
}

func NewEntryID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (q *Queue) bump() { q.Revision++ }

// Validate repairs inconsistent state (e.g. from an old persisted copy).
func (q *Queue) Validate() {
	if q.Repeat == "" {
		q.Repeat = RepeatOff
	}
	if len(q.Order) != len(q.Entries) {
		q.Order = identity(len(q.Entries))
	}
	if len(q.Entries) == 0 {
		q.Current = -1
	} else if q.Current < 0 || q.Current >= len(q.Order) {
		q.Current = 0
	}
}

// CurrentEntry returns the entry being played.
func (q *Queue) CurrentEntry() (Entry, bool) {
	if q.Current < 0 || q.Current >= len(q.Order) {
		return Entry{}, false
	}
	return q.Entries[q.Order[q.Current]], true
}

// Upcoming returns up to n entries after the current one in play order,
// honouring repeat-all wraparound.
func (q *Queue) Upcoming(n int) []Entry {
	out := []Entry{}
	if q.Current < 0 || len(q.Order) == 0 {
		return out
	}
	for i := 1; len(out) < n && i < len(q.Order)+1; i++ {
		pos := q.Current + i
		if pos >= len(q.Order) {
			if q.Repeat != RepeatAll {
				break
			}
			pos %= len(q.Order)
			if pos == q.Current {
				break
			}
		}
		out = append(out, q.Entries[q.Order[pos]])
	}
	return out
}

// Replace sets the whole queue and starts at startIndex (in list order).
func (q *Queue) Replace(trackIDs []string, startIndex int, addedBy string, max int) error {
	if len(trackIDs) > max {
		return ErrQueueFull
	}
	q.Entries = make([]Entry, 0, len(trackIDs))
	for _, id := range trackIDs {
		q.Entries = append(q.Entries, Entry{EntryID: NewEntryID(), TrackID: id, AddedBy: addedBy})
	}
	q.Order = identity(len(q.Entries))
	q.Current = -1
	if len(q.Entries) > 0 {
		if startIndex < 0 || startIndex >= len(q.Entries) {
			startIndex = 0
		}
		q.Current = startIndex
	}
	if q.Shuffle {
		q.reshuffle()
	}
	q.bump()
	return nil
}

// Add appends tracks to the end of the play order.
func (q *Queue) Add(trackIDs []string, addedBy string, max int) ([]Entry, error) {
	if len(q.Entries)+len(trackIDs) > max {
		return nil, ErrQueueFull
	}
	added := make([]Entry, 0, len(trackIDs))
	for _, id := range trackIDs {
		e := Entry{EntryID: NewEntryID(), TrackID: id, AddedBy: addedBy}
		q.Entries = append(q.Entries, e)
		q.Order = append(q.Order, len(q.Entries)-1)
		added = append(added, e)
	}
	if q.Current < 0 && len(q.Entries) > 0 {
		q.Current = 0
	}
	q.bump()
	return added, nil
}

// PlayNext inserts a track right after the current one in play order.
func (q *Queue) PlayNext(trackID, addedBy string, max int) (Entry, error) {
	if len(q.Entries)+1 > max {
		return Entry{}, ErrQueueFull
	}
	e := Entry{EntryID: NewEntryID(), TrackID: trackID, AddedBy: addedBy}
	q.Entries = append(q.Entries, e)
	idx := len(q.Entries) - 1
	pos := q.Current + 1
	if q.Current < 0 {
		pos = 0
		q.Current = 0
	}
	q.Order = append(q.Order[:pos], append([]int{idx}, q.Order[pos:]...)...)
	q.bump()
	return e, nil
}

func (q *Queue) indexOf(entryID string) int {
	for i, e := range q.Entries {
		if e.EntryID == entryID {
			return i
		}
	}
	return -1
}

func (q *Queue) positionOf(entryIdx int) int {
	for p, i := range q.Order {
		if i == entryIdx {
			return p
		}
	}
	return -1
}

// Remove deletes an entry. Removing the current entry makes the next one
// current (or the previous one at the end).
func (q *Queue) Remove(entryID string) error {
	idx := q.indexOf(entryID)
	if idx < 0 {
		return ErrEntryNotFound
	}
	pos := q.positionOf(idx)
	q.Entries = append(q.Entries[:idx], q.Entries[idx+1:]...)
	newOrder := make([]int, 0, len(q.Order)-1)
	for _, i := range q.Order {
		switch {
		case i == idx:
		case i > idx:
			newOrder = append(newOrder, i-1)
		default:
			newOrder = append(newOrder, i)
		}
	}
	q.Order = newOrder
	switch {
	case len(q.Entries) == 0:
		q.Current = -1
	case pos < q.Current:
		q.Current--
	case pos == q.Current && q.Current >= len(q.Order):
		q.Current = len(q.Order) - 1
	}
	q.bump()
	return nil
}

// Move moves an entry to position to in play order.
func (q *Queue) Move(entryID string, to int) error {
	idx := q.indexOf(entryID)
	if idx < 0 {
		return ErrEntryNotFound
	}
	if to < 0 || to >= len(q.Order) {
		return ErrOutOfRange
	}
	from := q.positionOf(idx)
	curIdx := -1
	if q.Current >= 0 {
		curIdx = q.Order[q.Current]
	}
	order := append(q.Order[:from:from], q.Order[from+1:]...)
	order = append(order[:to], append([]int{idx}, order[to:]...)...)
	q.Order = order
	if curIdx >= 0 {
		q.Current = q.positionOf(curIdx)
	}
	q.bump()
	return nil
}

// Jump makes entryID current.
func (q *Queue) Jump(entryID string) error {
	idx := q.indexOf(entryID)
	if idx < 0 {
		return ErrEntryNotFound
	}
	q.Current = q.positionOf(idx)
	q.bump()
	return nil
}

// Next advances and reports whether there is a next entry. With repeat-one
// a user-initiated skip still advances; the client repeats on natural end.
func (q *Queue) Next() bool {
	if len(q.Order) == 0 {
		return false
	}
	if q.Current+1 < len(q.Order) {
		q.Current++
		q.bump()
		return true
	}
	if q.Repeat == RepeatAll {
		q.Current = 0
		q.bump()
		return true
	}
	return false
}

// Previous steps back (wrapping with repeat-all).
func (q *Queue) Previous() bool {
	if len(q.Order) == 0 {
		return false
	}
	if q.Current > 0 {
		q.Current--
		q.bump()
		return true
	}
	if q.Repeat == RepeatAll {
		q.Current = len(q.Order) - 1
		q.bump()
		return true
	}
	return false
}

// SetShuffle turns shuffle on (keeping the current track first) or off
// (restoring list order and keeping the current track current).
func (q *Queue) SetShuffle(on bool) {
	if q.Shuffle == on {
		return
	}
	q.Shuffle = on
	if on {
		q.reshuffle()
	} else {
		cur := -1
		if q.Current >= 0 {
			cur = q.Order[q.Current]
		}
		q.Order = identity(len(q.Entries))
		if cur >= 0 {
			q.Current = cur
		}
	}
	q.bump()
}

func (q *Queue) SetRepeat(m RepeatMode) {
	q.Repeat = m
	q.bump()
}

// reshuffle randomizes everything except the current entry, which moves to
// the front so playback is not interrupted.
func (q *Queue) reshuffle() {
	n := len(q.Entries)
	if n == 0 {
		return
	}
	cur := -1
	if q.Current >= 0 && q.Current < len(q.Order) {
		cur = q.Order[q.Current]
	}
	rest := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if i != cur {
			rest = append(rest, i)
		}
	}
	mrand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	if cur >= 0 {
		q.Order = append([]int{cur}, rest...)
		q.Current = 0
	} else {
		q.Order = rest
	}
}

// TrackIDs returns track IDs in play order.
func (q *Queue) TrackIDs() []string {
	out := make([]string, 0, len(q.Order))
	for _, i := range q.Order {
		out = append(out, q.Entries[i].TrackID)
	}
	return out
}

// Clone returns a deep copy.
func (q *Queue) Clone() *Queue {
	c := *q
	c.Entries = append([]Entry(nil), q.Entries...)
	c.Order = append([]int(nil), q.Order...)
	return &c
}

func identity(n int) []int {
	o := make([]int, n)
	for i := range o {
		o[i] = i
	}
	return o
}
