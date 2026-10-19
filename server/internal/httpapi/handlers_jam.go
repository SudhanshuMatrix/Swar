package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"swar/internal/jam"
)

type jamCreateRequest struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"displayName"`
	Settings    jam.Settings `json:"settings"`
}

func (s *Server) handleJamCreate(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var req jamCreateRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	snap, err := s.jam.Create(device, req.Name, req.DisplayName, req.Settings)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	created(w, snap)
}

type jamJoinRequest struct {
	Code        string `json:"code"`
	DisplayName string `json:"displayName"`
}

func (s *Server) handleJamJoin(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var req jamJoinRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	snap, err := s.jam.Join(device, req.Code, req.DisplayName)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, snap)
}

func (s *Server) handleJamGet(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	snap, err := s.jam.Get(device, r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, snap)
}

func (s *Server) handleJamLeave(w http.ResponseWriter, r *http.Request, device string) {
	if err := s.jam.Leave(device, r.PathValue("id")); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"left": true})
}

func (s *Server) handleJamEnd(w http.ResponseWriter, r *http.Request, device string) {
	if _, err := s.jam.Execute(r.Context(), device, r.PathValue("id"), jam.Command{Type: "end"}); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"ended": true})
}

func (s *Server) handleJamCommand(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var cmd jam.Command
	if err := decode(r, &cmd); err != nil {
		s.writeErr(w, r, err)
		return
	}
	snap, err := s.jam.Execute(r.Context(), device, r.PathValue("id"), cmd)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if snap == nil {
		ok(w, map[string]any{"ended": true})
		return
	}
	ok(w, snap)
}

func (s *Server) handleJamParticipants(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	snap, err := s.jam.Get(device, r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"participants": snap.Participants, "removed": snap.Removed})
}

func (s *Server) handleJamRemoveParticipant(w http.ResponseWriter, r *http.Request, device string) {
	snap, err := s.jam.Execute(r.Context(), device, r.PathValue("id"), jam.Command{Type: "remove_participant", ParticipantID: r.PathValue("pid")})
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, snap)
}

// wsMessage is a client -> server realtime message: a Jam command plus a
// client-chosen id echoed in the ack/error, or one of "ping"/"resync".
type wsMessage struct {
	jam.Command
	ID         string `json:"id,omitempty"`
	ClientTime int64  `json:"clientTime,omitempty"`
}

const (
	wsReadLimit     = 16 << 10
	wsWriteTimeout  = 10 * time.Second
	wsPingInterval  = 25 * time.Second
	wsSyncInterval  = 5 * time.Second
	wsMsgsPerSecond = 10
	wsBurst         = 30
)

// handleJamWS upgrades to a WebSocket that carries Jam state and commands
// (never audio). Membership is re-checked on every command; on every
// (re)connect the client receives a full state message to resynchronize.
func (s *Server) handleJamWS(w http.ResponseWriter, r *http.Request, device string) {
	id := r.PathValue("id")
	// Reject non-members before upgrading so they get a normal HTTP error.
	if _, err := s.jam.Get(device, id); err != nil {
		s.writeErr(w, r, err)
		return
	}
	opts := &websocket.AcceptOptions{}
	if !s.allowAnyOrigin() {
		opts.OriginPatterns = s.originPatterns()
	} else {
		opts.InsecureSkipVerify = true // origin checks are moot: auth is a bearer token, not a cookie
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	c.SetReadLimit(wsReadLimit)
	conn, initial, err := s.jam.Connect(device, id)
	if err != nil {
		_, code, msg := statusFor(err)
		c.Close(websocket.StatusPolicyViolation, code+": "+msg)
		return
	}
	s.metrics.Inc("jam_ws_connections_total")
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	defer s.jam.Disconnect(id, conn)

	out := make(chan []byte, 16) // direct replies (acks, pongs, errors)
	out <- initial

	// Writer: the only goroutine that writes to the socket.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer cancel()
		ping := time.NewTicker(wsPingInterval)
		sync := time.NewTicker(wsSyncInterval)
		defer ping.Stop()
		defer sync.Stop()
		write := func(b []byte) bool {
			wctx, wcancel := context.WithTimeout(ctx, wsWriteTimeout)
			defer wcancel()
			return c.Write(wctx, websocket.MessageText, b) == nil
		}
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-out:
				if !write(b) {
					return
				}
			case b := <-conn.Send():
				if !write(b) {
					return
				}
			case <-conn.Done():
				// Flush what the server queued (e.g. "removed"/"ended").
				for {
					select {
					case b := <-conn.Send():
						write(b)
						continue
					default:
					}
					break
				}
				c.Close(websocket.StatusNormalClosure, conn.Reason())
				return
			case <-sync.C:
				if b, err := s.jam.SyncMessage(device, id); err == nil {
					if !write(b) {
						return
					}
				}
			case <-ping.C:
				pctx, pcancel := context.WithTimeout(ctx, wsWriteTimeout)
				err := c.Ping(pctx)
				pcancel()
				if err != nil {
					return
				}
			}
		}
	}()

	limiter := newRateLimiter(wsMsgsPerSecond, wsBurst)
	reply := func(v any) {
		b, _ := json.Marshal(v)
		select {
		case out <- b:
		case <-ctx.Done():
		}
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		if !limiter.allow("conn", 1) {
			reply(map[string]any{"type": "error", "code": "rate_limited", "message": "Too many messages"})
			continue
		}
		var m wsMessage
		if err := json.Unmarshal(data, &m); err != nil {
			reply(map[string]any{"type": "error", "code": "invalid_command", "message": "Messages must be JSON"})
			continue
		}
		switch m.Type {
		case "ping":
			reply(map[string]any{"type": "pong", "id": m.ID, "clientTime": m.ClientTime, "serverTime": time.Now().UnixMilli()})
			continue
		case "resync":
			b, err := s.jam.Resync(device, id)
			if err != nil {
				s.wsError(reply, m.ID, err)
				continue
			}
			select {
			case out <- b:
			case <-ctx.Done():
			}
			continue
		}
		cctx, ccancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
		snap, err := s.jam.Execute(cctx, device, id, m.Command)
		ccancel()
		if err != nil {
			s.wsError(reply, m.ID, err)
			var je *jam.Error
			if errors.As(err, &je) && je == jam.ErrStale {
				if b, err := s.jam.Resync(device, id); err == nil {
					select {
					case out <- b:
					case <-ctx.Done():
					}
				}
			}
			continue
		}
		rev := int64(0)
		if snap != nil {
			rev = snap.Rev
		}
		reply(map[string]any{"type": "ack", "id": m.ID, "rev": rev})
	}
	cancel()
	<-writerDone
	c.CloseNow()
}

func (s *Server) wsError(reply func(any), id string, err error) {
	_, code, msg := statusFor(err)
	reply(map[string]any{"type": "error", "id": id, "code": code, "message": msg})
}

func (s *Server) allowAnyOrigin() bool {
	for _, o := range s.cfg.CORSOrigins {
		if o == "*" {
			return true
		}
	}
	return false
}

// originPatterns converts configured origins (scheme://host) into the
// host patterns the WebSocket library expects.
func (s *Server) originPatterns() []string {
	out := []string{}
	for _, o := range s.cfg.CORSOrigins {
		if i := len("https://"); len(o) > i && o[:i] == "https://" {
			out = append(out, o[i:])
		} else if i := len("http://"); len(o) > i && o[:i] == "http://" {
			out = append(out, o[i:])
		}
	}
	return out
}
