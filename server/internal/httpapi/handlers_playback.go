package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"swar/internal/auth"
	"swar/internal/catalog"
	"swar/internal/playback"
	"swar/internal/radio"
	"swar/internal/recommendations"
	"swar/internal/repository"
)

func (s *Server) handlePlayback(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id := r.PathValue("id")
	d, err := s.pb.Resolve(r.Context(), id)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if t, err := s.cat.Track(r.Context(), id); err == nil {
		dd := *d
		dd.Track = t
		d = &dd
	}
	ok(w, d)
}

// handlePlaybackInvalidate lets a client report that a source failed (for
// example it expired early) so the next resolve fetches a fresh one.
func (s *Server) handlePlaybackInvalidate(w http.ResponseWriter, r *http.Request) {
	s.pb.Invalidate(r.PathValue("id"))
	ok(w, map[string]any{"invalidated": true})
}

type prefetchRequest struct {
	TrackIDs  []string `json:"trackIds"`
	DataSaver bool     `json:"dataSaver"`
}

func (s *Server) handlePrefetch(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req prefetchRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if len(req.TrackIDs) == 0 || len(req.TrackIDs) > 5 {
		s.writeErr(w, r, catalog.Invalid("trackIds must contain 1-5 ids (only the next one or two are prepared)"))
		return
	}
	owner := auth.Device(r.Context())
	if owner == "" {
		owner = "ip:" + s.clientIP(r)
	}
	res, err := s.pb.Prefetch(r.Context(), owner, req.TrackIDs, req.DataSaver)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"items": res})
}

// ---- personal queue ----

const maxPersonalQueue = 1000

type queueView struct {
	*playback.Queue
	Tracks   map[string]catalog.Track `json:"tracks"`
	Upcoming []string                 `json:"upcoming"` // next two track IDs, for prefetch
}

func (s *Server) loadQueue(r *http.Request, device string) (*playback.Queue, error) {
	data, err := s.store.LoadQueue(r.Context(), device)
	if errors.Is(err, repository.ErrNotFound) {
		return playback.NewQueue(), nil
	}
	if err != nil {
		return nil, err
	}
	q := playback.NewQueue()
	if err := json.Unmarshal(data, q); err != nil {
		return playback.NewQueue(), nil // corrupt state: start fresh
	}
	q.Validate()
	return q, nil
}

func (s *Server) saveQueue(r *http.Request, device string, q *playback.Queue) error {
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return s.store.SaveQueue(r.Context(), device, b, q.Revision)
}

func (s *Server) view(r *http.Request, q *playback.Queue) queueView {
	v := queueView{Queue: q, Tracks: map[string]catalog.Track{}, Upcoming: []string{}}
	ids := q.TrackIDs()
	if len(ids) > 100 {
		// Hydrate around the current position only; clients page the rest.
		start := max(0, q.Current-10)
		ids = ids[start:min(len(ids), start+100)]
	}
	if ts, err := s.cat.Tracks(r.Context(), ids); err == nil {
		for _, t := range ts {
			v.Tracks[t.ID] = t
		}
	}
	for _, e := range q.Upcoming(2) {
		v.Upcoming = append(v.Upcoming, e.TrackID)
	}
	return v
}

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	q, err := s.loadQueue(r, device)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, s.view(r, q))
}

type replaceQueueRequest struct {
	TrackIDs   []string `json:"trackIds"`
	StartIndex int      `json:"startIndex"`
	Shuffle    *bool    `json:"shuffle"`
	Repeat     string   `json:"repeat"`
}

func validIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		p, pid, ok := catalog.ParseID(id)
		if !ok {
			return nil, catalog.Invalid("invalid track id")
		}
		out = append(out, catalog.MakeID(p, pid))
	}
	return out, nil
}

func (s *Server) handleReplaceQueue(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var req replaceQueueRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ids, err := validIDs(req.TrackIDs)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	q, err := s.loadQueue(r, device)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if req.Shuffle != nil {
		q.Shuffle = *req.Shuffle
	}
	if req.Repeat != "" {
		rm, ok := playback.ParseRepeat(req.Repeat)
		if !ok {
			s.writeErr(w, r, catalog.Invalid("repeat must be off, all or one"))
			return
		}
		q.Repeat = rm
	}
	if err := q.Replace(ids, req.StartIndex, "", maxPersonalQueue); err != nil {
		s.writeErr(w, r, catalog.Invalid(err.Error()))
		return
	}
	if err := s.saveQueue(r, device, q); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, s.view(r, q))
}

type queueOpRequest struct {
	Op       string   `json:"op"`
	Rev      *int64   `json:"rev"`
	TrackIDs []string `json:"trackIds"`
	EntryID  string   `json:"entryId"`
	To       int      `json:"to"`
	Shuffle  bool     `json:"shuffle"`
	Repeat   string   `json:"repeat"`
}

// handleQueueOp applies one queue operation. When rev is supplied it must
// match the stored revision (optimistic concurrency between devices/tabs).
func (s *Server) handleQueueOp(w http.ResponseWriter, r *http.Request, device string) {
	noStore(w)
	var req queueOpRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	q, err := s.loadQueue(r, device)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if req.Rev != nil && *req.Rev != q.Revision {
		fail(w, http.StatusConflict, "stale_revision", "The queue changed; refresh it and retry")
		return
	}
	ids, err := validIDs(req.TrackIDs)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	switch req.Op {
	case "add":
		_, err = q.Add(ids, "", maxPersonalQueue)
	case "play_next":
		if len(ids) != 1 {
			err = catalog.Invalid("play_next takes exactly one track")
			break
		}
		_, err = q.PlayNext(ids[0], "", maxPersonalQueue)
	case "remove":
		err = q.Remove(req.EntryID)
	case "move":
		err = q.Move(req.EntryID, req.To)
	case "jump":
		err = q.Jump(req.EntryID)
	case "next":
		q.Next()
	case "previous":
		q.Previous()
	case "shuffle":
		q.SetShuffle(req.Shuffle)
	case "repeat":
		rm, valid := playback.ParseRepeat(req.Repeat)
		if !valid {
			err = catalog.Invalid("repeat must be off, all or one")
			break
		}
		q.SetRepeat(rm)
	case "clear":
		err = q.Replace(nil, 0, "", maxPersonalQueue)
	default:
		err = catalog.Invalid("unknown op")
	}
	if err != nil {
		var ce *catalog.Error
		if !errors.As(err, &ce) {
			err = catalog.Invalid(err.Error())
		}
		s.writeErr(w, r, err)
		return
	}
	if err := s.saveQueue(r, device, q); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, s.view(r, q))
}

// ---- recommendations ----

type recommendationsRequest struct {
	SeedTrackID     string   `json:"seedTrackId"`
	Exclude         []string `json:"exclude"`
	PlaylistContext []string `json:"playlistContext"`
	Limit           int      `json:"limit"`
	// Session lists tracks played earlier this session (newest first) and
	// Skipped the ones the listener skipped, so picks follow the session.
	Session []string `json:"session"`
	Skipped []string `json:"skipped"`
}

func (s *Server) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req recommendationsRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if len(req.Exclude) > 500 || len(req.PlaylistContext) > 200 || len(req.Session) > 50 || len(req.Skipped) > 200 {
		s.writeErr(w, r, catalog.Invalid("too many ids"))
		return
	}
	res, err := s.rec.Next(r.Context(), recommendations.Request{
		SeedTrackID: req.SeedTrackID, Exclude: req.Exclude, PlaylistContext: req.PlaylistContext,
		Session: req.Session, Skipped: req.Skipped,
		Limit: req.Limit, Device: auth.Device(r.Context()),
	})
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, res)
}

// ---- radio ----

type radioStartRequest struct {
	Kind  string `json:"kind"`
	Seed  string `json:"seed"`
	Count int    `json:"count"`
}

// radioOwner scopes sessions to the device, or to the IP for anonymous
// clients (personalized radio needs a device).
func (s *Server) radioOwner(r *http.Request) string {
	if d := auth.Device(r.Context()); d != "" {
		return d
	}
	return "ip:" + s.clientIP(r)
}

func (s *Server) handleRadioStart(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req radioStartRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	kind := radio.Kind(req.Kind)
	if kind == radio.KindPersonalized && auth.Device(r.Context()) == "" {
		fail(w, http.StatusUnauthorized, "unauthorized", "Personalized radio needs a device token")
		return
	}
	snap, err := s.radio.Start(r.Context(), s.radioOwner(r), kind, req.Seed, req.Count)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	created(w, snap)
}

func (s *Server) handleRadioGet(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	snap, err := s.radio.Get(s.radioOwner(r), r.PathValue("id"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, snap)
}

type radioNextRequest struct {
	Count int `json:"count"`
}

func (s *Server) handleRadioNext(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req radioNextRequest
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			s.writeErr(w, r, err)
			return
		}
	}
	snap, err := s.radio.Next(r.Context(), s.radioOwner(r), r.PathValue("id"), req.Count)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, snap)
}

type radioFeedbackRequest struct {
	TrackID string `json:"trackId"`
	Action  string `json:"action"`
}

func (s *Server) handleRadioFeedback(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req radioFeedbackRequest
	if err := decode(r, &req); err != nil {
		s.writeErr(w, r, err)
		return
	}
	snap, err := s.radio.Feedback(s.radioOwner(r), r.PathValue("id"), req.TrackID, req.Action)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, snap)
}

func (s *Server) handleRadioStop(w http.ResponseWriter, r *http.Request) {
	if err := s.radio.Stop(s.radioOwner(r), r.PathValue("id")); err != nil {
		s.writeErr(w, r, err)
		return
	}
	ok(w, map[string]any{"stopped": true})
}
