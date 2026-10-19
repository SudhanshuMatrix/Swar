// Package jam implements Swar Jam: small synchronized-listening sessions.
//
// The server is authoritative. It owns the shared queue, the playback
// state (track, playing, position anchored to server time) and the member
// list, and every command is checked against the *current* membership and
// role, so a removed participant's stale connection or token cannot act.
// Only state and commands are synchronized; audio is played by each client
// from its own authorized source. Sessions are in memory and do not survive
// a restart (documented).
package jam

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"swar/internal/catalog"
	"swar/internal/observability"
	"swar/internal/playback"
)

type Role string

const (
	RoleAdmin       Role = "admin"
	RoleParticipant Role = "participant"
)

type Settings struct {
	AllowParticipantAdd bool `json:"allowParticipantAdd"`
	AllowVoting         bool `json:"allowVoting"`
}

// Errors carry a stable code for clients.
type Error struct {
	Code    string
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

var (
	ErrInvalidCode   = &Error{"jam_not_found", "No Jam matches that code", 404}
	ErrNotFound      = &Error{"jam_not_found", "Jam not found", 404}
	ErrEnded         = &Error{"jam_ended", "This Jam has ended", 410}
	ErrExpired       = &Error{"jam_expired", "This Jam expired after being inactive", 410}
	ErrFull          = &Error{"jam_full", "This Jam is full", 409}
	ErrRemoved       = &Error{"removed_from_jam", "You were removed from this Jam", 403}
	ErrNotMember     = &Error{"not_a_member", "You are not in this Jam", 403}
	ErrAdminOnly     = &Error{"admin_only", "Only the Jam admin can do that", 403}
	ErrNotAllowed    = &Error{"not_allowed", "The admin has not enabled that for participants", 403}
	ErrStale         = &Error{"stale_revision", "The Jam changed; your view was refreshed", 409}
	ErrRateLimited   = &Error{"rate_limited", "Too many attempts; wait a minute and try again", 429}
	ErrQueueFull     = &Error{"queue_full", "The shared queue is full", 409}
	ErrBadCommand    = &Error{"invalid_command", "Invalid command", 400}
	ErrTooManyJams   = &Error{"too_many_jams", "The server is hosting too many Jams right now", 503}
	ErrUnknownTrack  = &Error{"unknown_track", "One or more tracks could not be found", 400}
	ErrEntryNotFound = &Error{"entry_not_found", "That queue item no longer exists", 404}
)

func badCommand(msg string) *Error { return &Error{"invalid_command", msg, 400} }

type Participant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	JoinedAt  time.Time `json:"joinedAt"`
	Connected bool      `json:"connected"`
	device    string
	conns     int
}

// PlaybackState is anchored to server time: while playing, the live
// position is PositionMs + (now - UpdatedAt).
type PlaybackState struct {
	EntryID    string `json:"entryId,omitempty"`
	TrackID    string `json:"trackId,omitempty"`
	Playing    bool   `json:"playing"`
	PositionMs int64  `json:"positionMs"`
	UpdatedAt  int64  `json:"updatedAt"` // server unix ms
}

type Session struct {
	mu           sync.Mutex
	id           string
	code         string
	name         string
	createdAt    time.Time
	lastActivity time.Time
	ended        bool
	endReason    string // "ended_by_admin", "host_left", "expired"
	endedAt      time.Time

	settings     Settings
	participants map[string]*Participant // by device
	removed      map[string]*Participant // by device; admin may allow rejoin
	queue        *playback.Queue
	tracks       map[string]catalog.Track
	votes        map[string]map[string]bool // entryID -> participantID set
	pb           PlaybackState
	rev          int64
	conns        map[*Conn]struct{}

	maxParticipants int
	maxQueue        int
	idleTTL         time.Duration
}

type TrackLookup interface {
	Tracks(ctx context.Context, ids []string) ([]catalog.Track, error)
}

type Options struct {
	MaxParticipants int
	MaxQueue        int
	IdleTTL         time.Duration
	MaxLifetime     time.Duration
	MaxSessions     int
}

type Manager struct {
	opt     Options
	tracks  TrackLookup
	m       *observability.Metrics
	now     func() time.Time
	mu      sync.Mutex
	byID    map[string]*Session
	byCode  map[string]*Session
	limiter *attemptLimiter
}

func NewManager(o Options, tracks TrackLookup, m *observability.Metrics) *Manager {
	if o.MaxSessions == 0 {
		o.MaxSessions = 1000
	}
	return &Manager{
		opt: o, tracks: tracks, m: m, now: time.Now,
		byID: map[string]*Session{}, byCode: map[string]*Session{},
		limiter: newAttemptLimiter(10, time.Minute),
	}
}

// SetClock overrides time (tests).
func (m *Manager) SetClock(now func() time.Time) { m.now = now; m.limiter.now = now }

const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O, 1/I/L
const codeLength = 6

func randomCode() string {
	b := make([]byte, codeLength)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b)
}

func randomID(prefix string, n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// NormalizeCode uppercases and strips spaces and dashes.
func NormalizeCode(c string) string {
	c = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(c)))
	if len(c) != codeLength {
		return ""
	}
	for _, r := range c {
		if !strings.ContainsRune(codeAlphabet, r) {
			return ""
		}
	}
	return c
}

func cleanName(n string) string {
	n = strings.Join(strings.Fields(n), " ")
	r := []rune(n)
	if len(r) > 40 {
		n = string(r[:40])
	}
	if n == "" {
		n = "Listener"
	}
	return n
}

// Create starts a Jam hosted by device. A device hosts at most one Jam: a
// previous one it hosts is ended.
func (m *Manager) Create(device, jamName, displayName string, settings Settings) (*Snapshot, error) {
	m.mu.Lock()
	active := 0
	var previous []*Session
	for _, s := range m.byID {
		s.mu.Lock()
		if !s.ended {
			active++
			if p := s.participants[device]; p != nil && p.Role == RoleAdmin {
				previous = append(previous, s)
			}
		}
		s.mu.Unlock()
	}
	if active-len(previous) >= m.opt.MaxSessions {
		m.mu.Unlock()
		return nil, ErrTooManyJams
	}
	var code string
	for i := 0; ; i++ {
		code = randomCode()
		if _, taken := m.byCode[code]; !taken {
			break
		}
		if i > 50 {
			m.mu.Unlock()
			return nil, ErrTooManyJams
		}
	}
	now := m.now()
	s := &Session{
		id: randomID("jam_", 16), code: code, name: cleanName(jamName), createdAt: now, lastActivity: now,
		settings: settings, participants: map[string]*Participant{}, removed: map[string]*Participant{},
		queue: playback.NewQueue(), tracks: map[string]catalog.Track{}, votes: map[string]map[string]bool{},
		conns: map[*Conn]struct{}{}, pb: PlaybackState{UpdatedAt: now.UnixMilli()},
		maxParticipants: m.opt.MaxParticipants, maxQueue: m.opt.MaxQueue, idleTTL: m.opt.IdleTTL,
	}
	if jamName == "" {
		s.name = cleanName(displayName) + "'s Jam"
	}
	s.participants[device] = &Participant{ID: randomID("p_", 8), Name: cleanName(displayName), Role: RoleAdmin, JoinedAt: now, device: device}
	m.byID[s.id] = s
	m.byCode[code] = s
	m.mu.Unlock()

	for _, p := range previous {
		m.end(p, "ended_by_admin")
	}
	m.m.Inc("jam_created_total")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(device, m.now()), nil
}

// Join adds device to the Jam with code.
func (m *Manager) Join(device, code, displayName string) (*Snapshot, error) {
	if !m.limiter.allow(device) {
		m.m.Inc(`jam_join_failures_total{reason="rate_limited"}`)
		return nil, ErrRateLimited
	}
	norm := NormalizeCode(code)
	m.mu.Lock()
	s := m.byCode[norm]
	m.mu.Unlock()
	if norm == "" || s == nil {
		m.m.Inc(`jam_join_failures_total{reason="invalid_code"}`)
		return nil, ErrInvalidCode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.endedErr(); err != nil {
		return nil, err
	}
	if _, ok := s.removed[device]; ok {
		m.m.Inc(`jam_join_failures_total{reason="removed"}`)
		return nil, ErrRemoved
	}
	now := m.now()
	if p, ok := s.participants[device]; ok {
		// Idempotent rejoin (e.g. after reconnecting).
		if n := cleanName(displayName); displayName != "" && n != p.Name {
			p.Name = n
			s.changed(now)
		}
		return s.snapshot(device, now), nil
	}
	if len(s.participants) >= m.opt.MaxParticipants {
		return nil, ErrFull
	}
	s.participants[device] = &Participant{ID: randomID("p_", 8), Name: cleanName(displayName), Role: RoleParticipant, JoinedAt: now, device: device}
	s.changed(now)
	m.m.Inc("jam_joins_total")
	m.broadcastLocked(s)
	return s.snapshot(device, now), nil
}

func (s *Session) endedErr() error {
	if !s.ended {
		return nil
	}
	if s.endReason == "expired" {
		return ErrExpired
	}
	return ErrEnded
}

func (s *Session) changed(now time.Time) {
	s.rev++
	s.lastActivity = now
}

func (m *Manager) session(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// member returns the caller's live membership. Must hold s.mu.
func (s *Session) member(device string) (*Participant, error) {
	if err := s.endedErr(); err != nil {
		return nil, err
	}
	if _, ok := s.removed[device]; ok {
		return nil, ErrRemoved
	}
	p, ok := s.participants[device]
	if !ok {
		return nil, ErrNotMember
	}
	return p, nil
}

// Get returns the caller's view of the Jam.
func (m *Manager) Get(device, id string) (*Snapshot, error) {
	s, err := m.session(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.member(device); err != nil {
		return nil, err
	}
	return s.snapshot(device, m.now()), nil
}

// Leave removes the caller. If the admin leaves, the Jam ends.
func (m *Manager) Leave(device, id string) error {
	s, err := m.session(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	p, err := s.member(device)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if p.Role == RoleAdmin {
		s.mu.Unlock()
		m.end(s, "host_left")
		return nil
	}
	delete(s.participants, device)
	for _, v := range s.votes {
		delete(v, p.ID)
	}
	s.changed(m.now())
	m.closeConnsLocked(s, device, "left")
	m.broadcastLocked(s)
	s.mu.Unlock()
	return nil
}

// Command is a client request to change shared state.
type Command struct {
	Type          string    `json:"type"`
	Rev           *int64    `json:"rev,omitempty"`
	TrackIDs      []string  `json:"trackIds,omitempty"`
	EntryID       string    `json:"entryId,omitempty"`
	To            *int      `json:"to,omitempty"`
	PositionMs    *int64    `json:"positionMs,omitempty"`
	Shuffle       *bool     `json:"shuffle,omitempty"`
	Repeat        string    `json:"repeat,omitempty"`
	ParticipantID string    `json:"participantId,omitempty"`
	Settings      *Settings `json:"settings,omitempty"`
}

var adminOnly = map[string]bool{
	"play": true, "pause": true, "seek": true, "next": true, "previous": true, "jump": true,
	"shuffle": true, "repeat": true, "queue_move": true, "queue_play_next": true, "track_ended": true,
	"remove_participant": true, "allow_rejoin": true, "update_settings": true, "end": true,
}

// commands that must be based on the latest revision when Rev is given.
// Additive commands (queue_add, vote) commute, so they are always applied.
var revChecked = map[string]bool{
	"play": true, "pause": true, "seek": true, "next": true, "previous": true, "jump": true,
	"queue_move": true, "queue_remove": true, "track_ended": true,
}

// Execute validates and applies a command for device.
func (m *Manager) Execute(ctx context.Context, device, id string, cmd Command) (*Snapshot, error) {
	s, err := m.session(id)
	if err != nil {
		return nil, err
	}

	// Resolve track metadata before taking the session lock (network).
	var newTracks []catalog.Track
	if cmd.Type == "queue_add" || cmd.Type == "queue_play_next" {
		if len(cmd.TrackIDs) == 0 || len(cmd.TrackIDs) > 50 {
			return nil, badCommand("trackIds must contain 1-50 ids")
		}
		for _, tid := range cmd.TrackIDs {
			if _, _, ok := catalog.ParseID(tid); !ok {
				return nil, badCommand("invalid track id")
			}
		}
		// Check membership first so non-members cannot trigger lookups.
		s.mu.Lock()
		_, err := s.member(device)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		newTracks, err = m.tracks.Tracks(ctx, cmd.TrackIDs)
		if err != nil || len(newTracks) != len(dedupeIDs(cmd.TrackIDs)) {
			return nil, ErrUnknownTrack
		}
	}

	s.mu.Lock()
	if cmd.Type == "end" {
		p, err := s.member(device)
		if err == nil && p.Role != RoleAdmin {
			err = ErrAdminOnly
		}
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		m.end(s, "ended_by_admin")
		return nil, nil
	}
	defer s.mu.Unlock()
	p, err := s.member(device)
	if err != nil {
		return nil, err
	}
	if adminOnly[cmd.Type] && p.Role != RoleAdmin {
		m.m.Inc(`jam_rejected_total{reason="admin_only"}`)
		return nil, ErrAdminOnly
	}
	if cmd.Rev != nil && revChecked[cmd.Type] && *cmd.Rev != s.rev {
		m.m.Inc(`jam_rejected_total{reason="stale"}`)
		return nil, ErrStale
	}
	now := m.now()
	if err := m.apply(s, p, cmd, newTracks, now); err != nil {
		return nil, err
	}
	s.changed(now)
	m.broadcastLocked(s)
	return s.snapshot(device, now), nil
}

func (m *Manager) apply(s *Session, p *Participant, cmd Command, newTracks []catalog.Track, now time.Time) error {
	nowMs := now.UnixMilli()
	q := s.queue
	switch cmd.Type {
	case "play":
		if _, ok := q.CurrentEntry(); !ok {
			return badCommand("the queue is empty")
		}
		if !s.pb.Playing {
			s.pb.Playing, s.pb.UpdatedAt = true, nowMs
		}
	case "pause":
		if s.pb.Playing {
			s.pb.PositionMs = s.livePosition(nowMs)
			s.pb.Playing, s.pb.UpdatedAt = false, nowMs
		}
	case "seek":
		if cmd.PositionMs == nil || *cmd.PositionMs < 0 || *cmd.PositionMs > 24*3600_000 {
			return badCommand("positionMs is required")
		}
		s.pb.PositionMs, s.pb.UpdatedAt = *cmd.PositionMs, nowMs
	case "next", "track_ended":
		if cmd.Type == "track_ended" {
			// Ignore late reports about a track that is no longer current.
			if cur, ok := q.CurrentEntry(); !ok || cmd.EntryID != cur.EntryID {
				return nil
			}
			if q.Repeat == playback.RepeatOne {
				s.pb.PositionMs, s.pb.UpdatedAt = 0, nowMs
				return nil
			}
		}
		if q.Next() {
			s.setCurrent(nowMs)
		} else {
			s.pb.Playing, s.pb.PositionMs, s.pb.UpdatedAt = false, 0, nowMs
		}
	case "previous":
		if s.livePosition(nowMs) > 3000 || !q.Previous() {
			s.pb.PositionMs, s.pb.UpdatedAt = 0, nowMs
		} else {
			s.setCurrent(nowMs)
		}
	case "jump":
		if err := q.Jump(cmd.EntryID); err != nil {
			return ErrEntryNotFound
		}
		s.setCurrent(nowMs)
		s.pb.Playing = true
	case "shuffle":
		if cmd.Shuffle == nil {
			return badCommand("shuffle is required")
		}
		q.SetShuffle(*cmd.Shuffle)
		s.applyVotes()
	case "repeat":
		r, ok := playback.ParseRepeat(cmd.Repeat)
		if !ok {
			return badCommand("repeat must be off, all or one")
		}
		q.SetRepeat(r)
	case "queue_add", "queue_play_next":
		if p.Role != RoleAdmin && !s.settings.AllowParticipantAdd {
			return ErrNotAllowed
		}
		wasEmpty := len(q.Entries) == 0
		for _, t := range newTracks {
			s.tracks[t.ID] = t
		}
		ids := make([]string, 0, len(newTracks))
		for _, t := range newTracks {
			ids = append(ids, t.ID)
		}
		if cmd.Type == "queue_play_next" {
			for i := len(ids) - 1; i >= 0; i-- {
				if _, err := q.PlayNext(ids[i], p.ID, m.opt.MaxQueue); err != nil {
					return ErrQueueFull
				}
			}
		} else if _, err := q.Add(ids, p.ID, m.opt.MaxQueue); err != nil {
			return ErrQueueFull
		}
		if wasEmpty {
			s.setCurrent(nowMs)
		}
		s.applyVotes()
	case "queue_remove":
		idx := -1
		for i, e := range q.Entries {
			if e.EntryID == cmd.EntryID {
				idx = i
			}
		}
		if idx < 0 {
			return ErrEntryNotFound
		}
		if p.Role != RoleAdmin && q.Entries[idx].AddedBy != p.ID {
			return ErrAdminOnly
		}
		wasCurrent := s.pb.EntryID == cmd.EntryID
		_ = q.Remove(cmd.EntryID)
		delete(s.votes, cmd.EntryID)
		if wasCurrent {
			s.setCurrent(nowMs)
		}
	case "queue_move":
		if cmd.To == nil {
			return badCommand("to is required")
		}
		if err := q.Move(cmd.EntryID, *cmd.To); err != nil {
			if errors.Is(err, playback.ErrEntryNotFound) {
				return ErrEntryNotFound
			}
			return badCommand("position out of range")
		}
	case "vote", "unvote":
		if !s.settings.AllowVoting {
			return ErrNotAllowed
		}
		found := false
		for _, e := range q.Entries {
			if e.EntryID == cmd.EntryID {
				found = true
			}
		}
		if !found {
			return ErrEntryNotFound
		}
		if s.votes[cmd.EntryID] == nil {
			s.votes[cmd.EntryID] = map[string]bool{}
		}
		if cmd.Type == "vote" {
			s.votes[cmd.EntryID][p.ID] = true
		} else {
			delete(s.votes[cmd.EntryID], p.ID)
		}
		s.applyVotes()
	case "remove_participant":
		dev, target := s.byParticipantID(cmd.ParticipantID)
		if target == nil {
			return badCommand("participant not found")
		}
		if target.Role == RoleAdmin {
			return badCommand("the admin cannot be removed")
		}
		delete(s.participants, dev)
		target.Connected, target.conns = false, 0
		s.removed[dev] = target
		for _, v := range s.votes {
			delete(v, target.ID)
		}
		m.closeConnsLocked(s, dev, "removed")
		m.m.Inc("jam_removals_total")
	case "allow_rejoin":
		for dev, r := range s.removed {
			if r.ID == cmd.ParticipantID {
				delete(s.removed, dev)
				return nil
			}
		}
		return badCommand("removed participant not found")
	case "update_settings":
		if cmd.Settings == nil {
			return badCommand("settings is required")
		}
		s.settings = *cmd.Settings
		if !s.settings.AllowVoting {
			s.votes = map[string]map[string]bool{}
		}
	default:
		return badCommand("unknown command type")
	}
	return nil
}

// setCurrent points playback at the queue's current entry from position 0.
func (s *Session) setCurrent(nowMs int64) {
	e, ok := s.queue.CurrentEntry()
	if !ok {
		s.pb = PlaybackState{UpdatedAt: nowMs}
		return
	}
	s.pb.EntryID, s.pb.TrackID, s.pb.PositionMs, s.pb.UpdatedAt = e.EntryID, e.TrackID, 0, nowMs
}

func (s *Session) livePosition(nowMs int64) int64 {
	if !s.pb.Playing {
		return s.pb.PositionMs
	}
	return s.pb.PositionMs + max(0, nowMs-s.pb.UpdatedAt)
}

// applyVotes stably reorders the upcoming part of the queue by vote count.
func (s *Session) applyVotes() {
	if !s.settings.AllowVoting || s.queue.Current < 0 {
		return
	}
	q := s.queue
	up := q.Order[q.Current+1:]
	count := func(i int) int { return len(s.votes[q.Entries[i].EntryID]) }
	sort.SliceStable(up, func(a, b int) bool { return count(up[a]) > count(up[b]) })
}

func (s *Session) byParticipantID(pid string) (string, *Participant) {
	for dev, p := range s.participants {
		if p.ID == pid {
			return dev, p
		}
	}
	return "", nil
}

// end marks the Jam ended, notifies and disconnects everyone. The code is
// kept for a while so joiners get "ended" rather than "invalid code".
func (m *Manager) end(s *Session, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended, s.endReason, s.endedAt = true, reason, m.now()
	s.rev++
	msg, _ := json.Marshal(map[string]any{"type": "ended", "reason": reason})
	for c := range s.conns {
		c.sendAndClose(msg, "ended")
	}
	s.conns = map[*Conn]struct{}{}
	m.m.Inc("jam_ended_total")
}

// GC expires idle Jams and forgets long-ended ones. Call periodically.
func (m *Manager) GC() {
	now := m.now()
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		ended, endedAt := s.ended, s.endedAt
		idle := len(s.conns) == 0 && now.Sub(s.lastActivity) > m.opt.IdleTTL
		tooOld := now.Sub(s.createdAt) > m.opt.MaxLifetime
		s.mu.Unlock()
		switch {
		case ended && now.Sub(endedAt) > time.Hour:
			m.mu.Lock()
			delete(m.byID, s.id)
			if m.byCode[s.code] == s {
				delete(m.byCode, s.code)
			}
			m.mu.Unlock()
		case !ended && (idle || tooOld):
			m.end(s, "expired")
		}
	}
	m.limiter.gc()
}

// ActiveCount returns the number of live Jams.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.byID {
		s.mu.Lock()
		if !s.ended {
			n++
		}
		s.mu.Unlock()
	}
	return n
}

func dedupeIDs(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		p, pid, _ := catalog.ParseID(id)
		k := catalog.MakeID(p, pid)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// attemptLimiter is a fixed-window limiter for join attempts per device.
type attemptLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{limit: limit, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

func (l *attemptLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	h := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			h = append(h, t)
		}
	}
	if len(h) >= l.limit {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	return true
}

func (l *attemptLimiter) gc() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, h := range l.hits {
		if len(h) == 0 || now.Sub(h[len(h)-1]) > l.window {
			delete(l.hits, k)
		}
	}
}
