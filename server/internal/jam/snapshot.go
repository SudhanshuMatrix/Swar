package jam

import (
	"encoding/json"
	"sync"
	"time"

	"swar/internal/catalog"
)

type ParticipantView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	Connected bool      `json:"connected"`
	JoinedAt  time.Time `json:"joinedAt"`
}

type EntryView struct {
	EntryID string        `json:"entryId"`
	Track   catalog.Track `json:"track"`
	AddedBy string        `json:"addedBy,omitempty"`
	Votes   int           `json:"votes"`
	Voted   bool          `json:"voted"`
}

type QueueView struct {
	Entries []EntryView `json:"entries"` // in play order
	Current int         `json:"current"` // index into entries, -1 if empty
	Shuffle bool        `json:"shuffle"`
	Repeat  string      `json:"repeat"`
}

type NowPlaying struct {
	PlaybackState
	Track *catalog.Track `json:"track,omitempty"`
}

// Snapshot is one member's view of the Jam.
type Snapshot struct {
	ID           string            `json:"id"`
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	Rev          int64             `json:"rev"`
	ServerTime   int64             `json:"serverTime"`
	You          string            `json:"you"`
	Role         Role              `json:"role"`
	Settings     Settings          `json:"settings"`
	Participants []ParticipantView `json:"participants"`
	Removed      []ParticipantView `json:"removed,omitempty"` // admin only
	Queue        QueueView         `json:"queue"`
	NowPlaying   NowPlaying        `json:"nowPlaying"`
	Limits       map[string]int    `json:"limits"`
	ExpiresAfter string            `json:"expiresAfterIdle"`
}

func (s *Session) snapshot(device string, now time.Time) *Snapshot {
	me := s.participants[device]
	snap := &Snapshot{
		ID: s.id, Code: s.code, Name: s.name, Rev: s.rev, ServerTime: now.UnixMilli(),
		Settings: s.settings, Participants: []ParticipantView{},
		NowPlaying:   NowPlaying{PlaybackState: s.pb},
		Limits:       map[string]int{"maxParticipants": s.maxParticipants, "maxQueue": s.maxQueue},
		ExpiresAfter: s.idleTTL.String(),
	}
	if me != nil {
		snap.You, snap.Role = me.ID, me.Role
	}
	for _, p := range s.participants {
		snap.Participants = append(snap.Participants, ParticipantView{ID: p.ID, Name: p.Name, Role: p.Role, Connected: p.conns > 0, JoinedAt: p.JoinedAt})
	}
	sortParticipants(snap.Participants)
	if me != nil && me.Role == RoleAdmin {
		for _, r := range s.removed {
			snap.Removed = append(snap.Removed, ParticipantView{ID: r.ID, Name: r.Name, Role: r.Role, JoinedAt: r.JoinedAt})
		}
		sortParticipants(snap.Removed)
	}
	q := s.queue
	snap.Queue = QueueView{Entries: []EntryView{}, Current: q.Current, Shuffle: q.Shuffle, Repeat: string(q.Repeat)}
	for _, idx := range q.Order {
		e := q.Entries[idx]
		t := s.tracks[e.TrackID]
		if t.ID == "" {
			t = catalog.Track{ID: e.TrackID, Artists: []catalog.ArtistRef{}, Images: []catalog.Image{}}
		}
		ev := EntryView{EntryID: e.EntryID, Track: t, AddedBy: e.AddedBy, Votes: len(s.votes[e.EntryID])}
		if me != nil {
			ev.Voted = s.votes[e.EntryID][me.ID]
		}
		snap.Queue.Entries = append(snap.Queue.Entries, ev)
	}
	if s.pb.TrackID != "" {
		if t, ok := s.tracks[s.pb.TrackID]; ok {
			snap.NowPlaying.Track = &t
		}
	}
	// Report the live position so late joiners start in the right place.
	snap.NowPlaying.PositionMs = s.livePosition(now.UnixMilli())
	snap.NowPlaying.UpdatedAt = now.UnixMilli()
	return snap
}

func sortParticipants(ps []ParticipantView) {
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && less(ps[j], ps[j-1]); j-- {
			ps[j], ps[j-1] = ps[j-1], ps[j]
		}
	}
}

func less(a, b ParticipantView) bool {
	if a.Role != b.Role {
		return a.Role == RoleAdmin
	}
	return a.JoinedAt.Before(b.JoinedAt)
}

// Conn is one realtime connection. The transport (WebSocket) reads from
// Send and calls Manager methods for incoming messages.
type Conn struct {
	device    string
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
	reason    string
}

// Send is the outbound message stream; it is closed when the connection
// must be dropped (removed, ended, too slow).
func (c *Conn) Send() <-chan []byte { return c.send }

// Done is closed when the server drops the connection.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) Reason() string { return c.reason }

func (c *Conn) close(reason string) {
	c.closeOnce.Do(func() {
		c.reason = reason
		close(c.done)
	})
}

// trySend queues msg without blocking; a full buffer means the client is
// too slow, so it is disconnected and will resync on reconnect.
func (c *Conn) trySend(msg []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- msg:
		return true
	default:
		c.close("slow_consumer")
		return false
	}
}

func (c *Conn) sendAndClose(msg []byte, reason string) {
	c.trySend(msg)
	c.close(reason)
}

// Connect registers a realtime connection for a member and returns the
// initial state message (state resync on every (re)connect).
func (m *Manager) Connect(device, id string) (*Conn, []byte, error) {
	s, err := m.session(id)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.member(device)
	if err != nil {
		return nil, nil, err
	}
	c := &Conn{device: device, send: make(chan []byte, 64), done: make(chan struct{})}
	p.conns++
	p.Connected = true
	if p.conns == 1 {
		// Tell the others about the presence change before registering
		// this connection; it gets the state message returned below.
		s.changed(m.now())
		m.broadcastLocked(s)
	}
	s.conns[c] = struct{}{}
	return c, stateMessage(s.snapshot(device, m.now())), nil
}

// Disconnect unregisters a connection and updates presence.
func (m *Manager) Disconnect(id string, c *Conn) {
	s, err := m.session(id)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.conns[c]; !ok {
		return
	}
	delete(s.conns, c)
	c.close("disconnected")
	if p, ok := s.participants[c.device]; ok {
		p.conns = max(0, p.conns-1)
		p.Connected = p.conns > 0
		if !p.Connected && !s.ended {
			s.changed(m.now())
			m.broadcastLocked(s)
		}
	}
}

// Resync returns a fresh state message for a connection's member.
func (m *Manager) Resync(device, id string) ([]byte, error) {
	snap, err := m.Get(device, id)
	if err != nil {
		return nil, err
	}
	return stateMessage(snap), nil
}

// SyncMessage returns a lightweight playback correction for members.
func (m *Manager) SyncMessage(device, id string) ([]byte, error) {
	s, err := m.session(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.member(device); err != nil {
		return nil, err
	}
	now := m.now().UnixMilli()
	pb := s.pb
	pb.PositionMs, pb.UpdatedAt = s.livePosition(now), now
	return json.Marshal(map[string]any{"type": "sync", "rev": s.rev, "serverTime": now, "playback": pb})
}

func stateMessage(snap *Snapshot) []byte {
	b, _ := json.Marshal(map[string]any{"type": "state", "rev": snap.Rev, "serverTime": snap.ServerTime, "jam": snap})
	return b
}

// broadcastLocked sends each connection its own view. Must hold s.mu.
func (m *Manager) broadcastLocked(s *Session) {
	now := m.now()
	views := map[string][]byte{}
	for c := range s.conns {
		msg, ok := views[c.device]
		if !ok {
			if _, member := s.participants[c.device]; !member {
				continue
			}
			msg = stateMessage(s.snapshot(c.device, now))
			views[c.device] = msg
		}
		if !c.trySend(msg) {
			delete(s.conns, c)
			if p, ok := s.participants[c.device]; ok {
				p.conns = max(0, p.conns-1)
				p.Connected = p.conns > 0
			}
		}
	}
}

// closeConnsLocked disconnects every connection of device. Must hold s.mu.
func (m *Manager) closeConnsLocked(s *Session, device, reason string) {
	msg, _ := json.Marshal(map[string]any{"type": reason})
	for c := range s.conns {
		if c.device == device {
			c.sendAndClose(msg, reason)
			delete(s.conns, c)
		}
	}
}
