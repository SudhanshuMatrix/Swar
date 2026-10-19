// Package downloads tracks client-side offline copies. The server decides
// eligibility and records status; it never receives, stores or relays the
// audio. A track only becomes "completed" after the client reports a size
// and checksum for the verified file, so cached metadata can never be
// mistaken for a downloaded track.
package downloads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"

	"swar/internal/catalog"
	"swar/internal/playback"
	"swar/internal/repository"
)

const (
	StatusQueued      = "queued"
	StatusDownloading = "downloading"
	StatusPaused      = "paused"
	StatusVerifying   = "verifying"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusCanceled    = "canceled"
)

// transitions lists the allowed next states for each state. "" is the
// state of a track with no download row.
var transitions = map[string][]string{
	"":                {StatusQueued},
	StatusQueued:      {StatusDownloading, StatusCanceled, StatusFailed},
	StatusDownloading: {StatusDownloading, StatusPaused, StatusVerifying, StatusFailed, StatusCanceled},
	StatusPaused:      {StatusDownloading, StatusCanceled},
	StatusVerifying:   {StatusCompleted, StatusFailed},
	StatusFailed:      {StatusQueued, StatusCanceled},
	StatusCanceled:    {StatusQueued},
	StatusCompleted:   {StatusFailed}, // client found the file missing/corrupt
}

// CanTransition reports whether from -> to is allowed.
func CanTransition(from, to string) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

var sha256Re = regexp.MustCompile(`^[a-f0-9]{64}$`)

// LocalID is a stable, device-independent ID for the offline library.
func LocalID(trackID string) string {
	h := sha256.Sum256([]byte(trackID))
	return "dl_" + hex.EncodeToString(h[:8])
}

type Service struct {
	store    *repository.Store
	cat      *catalog.Service
	resolver *playback.Resolver
}

func New(store *repository.Store, cat *catalog.Service, r *playback.Resolver) *Service {
	return &Service{store: store, cat: cat, resolver: r}
}

type Eligibility struct {
	TrackID    string               `json:"trackId"`
	LocalID    string               `json:"localId"`
	Eligible   bool                 `json:"eligible"`
	Reason     string               `json:"reason"`
	Track      *catalog.Track       `json:"track,omitempty"`
	Descriptor *playback.Descriptor `json:"descriptor,omitempty"`
}

// Eligibility checks whether a track may be kept offline and, if so,
// returns the authorized source the client should download from directly.
func (s *Service) Eligibility(ctx context.Context, trackID string) (*Eligibility, error) {
	t, err := s.cat.Track(ctx, trackID)
	if err != nil {
		return nil, err
	}
	e := &Eligibility{TrackID: t.ID, LocalID: LocalID(t.ID), Eligible: t.Download.Eligible, Reason: t.Download.Reason, Track: t}
	if !e.Eligible {
		return e, nil
	}
	d, err := s.resolver.Resolve(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if !d.Available {
		e.Eligible, e.Reason = false, "no_authorized_source"
		return e, nil
	}
	e.Descriptor = d
	return e, nil
}

type Update struct {
	Status     string `json:"status"`
	BytesTotal int64  `json:"bytesTotal"`
	BytesDone  int64  `json:"bytesDone"`
	SHA256     string `json:"sha256"`
	Error      string `json:"error"`
}

var ErrConflict = errors.New("download state changed concurrently")

// Report applies a client status update after validating the transition.
func (s *Service) Report(ctx context.Context, device, trackID string, u Update) (*repository.Download, error) {
	p, pid, ok := catalog.ParseID(trackID)
	if !ok {
		return nil, catalog.Invalid("invalid track id")
	}
	trackID = catalog.MakeID(p, pid)
	cur, err := s.store.Download(ctx, device, trackID)
	from := ""
	switch {
	case errors.Is(err, repository.ErrNotFound):
	case err != nil:
		return nil, err
	default:
		from = cur.Status
	}
	if !CanTransition(from, u.Status) {
		return nil, catalog.Invalid("cannot change download from " + orNone(from) + " to " + u.Status)
	}
	if u.BytesTotal < 0 || u.BytesDone < 0 || (u.BytesTotal > 0 && u.BytesDone > u.BytesTotal) {
		return nil, catalog.Invalid("invalid byte counts")
	}
	if len(u.Error) > 200 {
		u.Error = u.Error[:200]
	}
	if u.Status == StatusQueued {
		// (Re)queueing re-checks eligibility so a source that stopped
		// permitting downloads cannot be queued again.
		el, err := s.Eligibility(ctx, trackID)
		if err != nil {
			return nil, err
		}
		if !el.Eligible {
			return nil, catalog.Unavailable("this track cannot be downloaded: " + el.Reason)
		}
	}
	if u.Status == StatusCompleted {
		if u.BytesTotal <= 0 || u.BytesDone != u.BytesTotal {
			return nil, catalog.Invalid("completed downloads must report a matching, non-zero size")
		}
		if !sha256Re.MatchString(u.SHA256) {
			return nil, catalog.Invalid("completed downloads must report the file's sha256")
		}
	} else if u.Status != StatusVerifying {
		u.SHA256 = ""
	}
	d := repository.Download{
		TrackID: trackID, LocalID: LocalID(trackID), Status: u.Status,
		BytesTotal: u.BytesTotal, BytesDone: u.BytesDone, SHA256: u.SHA256, Error: u.Error,
	}
	if u.Status != StatusFailed {
		d.Error = ""
	}
	if err := s.store.PutDownload(ctx, device, d, from); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, catalog.NewError(catalog.KindInvalid, "download state changed concurrently; refresh and retry", ErrConflict)
		}
		return nil, err
	}
	if t, err := s.cat.Track(ctx, trackID); err == nil {
		_ = s.store.SaveTracks(ctx, *t)
	}
	return s.store.Download(ctx, device, trackID)
}

func (s *Service) List(ctx context.Context, device string) ([]repository.Download, error) {
	return s.store.Downloads(ctx, device)
}

// Remove forgets a download after the client deleted the file.
func (s *Service) Remove(ctx context.Context, device, trackID string) error {
	p, pid, ok := catalog.ParseID(trackID)
	if !ok {
		return catalog.Invalid("invalid track id")
	}
	err := s.store.DeleteDownload(ctx, device, catalog.MakeID(p, pid))
	if errors.Is(err, repository.ErrNotFound) {
		return catalog.NotFound("download not found")
	}
	return err
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
