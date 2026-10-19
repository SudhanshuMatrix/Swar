// Package repository persists per-device library data in SQLite. SQLite
// suits Swar's single-instance, modest-server deployment; switching to
// PostgreSQL later only touches this package.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo

	"swar/internal/catalog"
	"swar/migrations"
)

var ErrNotFound = errors.New("not found")

var memSeq atomic.Int64

type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies
// migrations. Use ":memory:" for tests.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
		dsn = "file:" + path
	} else {
		// A unique name per Open keeps parallel tests isolated.
		dsn = fmt.Sprintf("file:mem%d?mode=memory&cache=shared", memSeq.Add(1))
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a small pool avoids lock contention.
	db.SetMaxOpenConns(4)
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)`, name, s.now().Unix()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ms() int64 { return s.now().UnixMilli() }

// ---- devices ----

func (s *Store) CreateDevice(ctx context.Context, id string) error {
	now := s.ms()
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id, created_at, last_seen_at) VALUES (?, ?, ?)`, id, now, now)
	return err
}

func (s *Store) DeviceExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) TouchDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE id = ?`, s.ms(), id)
	return err
}

// DeleteDevice removes a device and (by cascade) all of its data.
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	return err
}

// ---- track metadata snapshots ----

func (s *Store) SaveTracks(ctx context.Context, tracks ...catalog.Track) error {
	if len(tracks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO track_meta(track_id, data, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(track_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range tracks {
		if t.ID == "" {
			continue
		}
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, t.ID, string(b), s.ms()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Tracks returns stored metadata for ids (missing ones are skipped).
func (s *Store) Tracks(ctx context.Context, ids []string) (map[string]catalog.Track, error) {
	out := make(map[string]catalog.Track, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		q := `SELECT track_id, data FROM track_meta WHERE track_id IN (` + placeholders(len(chunk)) + `)`
		rows, err := s.db.QueryContext(ctx, q, anys(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, data string
			if err := rows.Scan(&id, &data); err != nil {
				rows.Close()
				return nil, err
			}
			var t catalog.Track
			if json.Unmarshal([]byte(data), &t) == nil {
				out[id] = t
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- likes / dislikes ----

type Reaction string

const (
	Like    Reaction = "likes"
	Dislike Reaction = "dislikes"
)

// SetReaction stores a like or dislike; the two are mutually exclusive.
func (s *Store) SetReaction(ctx context.Context, device, trackID string, r Reaction) error {
	other := Dislike
	if r == Dislike {
		other = Like
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+string(other)+` WHERE device_id = ? AND track_id = ?`, device, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO `+string(r)+`(device_id, track_id, created_at) VALUES (?, ?, ?)`, device, trackID, s.ms()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClearReaction(ctx context.Context, device, trackID string, r Reaction) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+string(r)+` WHERE device_id = ? AND track_id = ?`, device, trackID)
	return err
}

// Reactions returns track IDs newest first.
func (s *Store) Reactions(ctx context.Context, device string, r Reaction, limit int) ([]string, error) {
	return s.strings(ctx, `SELECT track_id FROM `+string(r)+` WHERE device_id = ? ORDER BY created_at DESC LIMIT ?`, device, limit)
}

// ---- playlists ----

type Playlist struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	TrackIDs  []string `json:"trackIds"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

const MaxPlaylistTracks = 5000

func (s *Store) CreatePlaylist(ctx context.Context, device, id, name string) (*Playlist, error) {
	now := s.ms()
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlists(id, device_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, device, name, now, now)
	if err != nil {
		return nil, err
	}
	return &Playlist{ID: id, Name: name, TrackIDs: []string{}, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) Playlists(ctx context.Context, device string) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, updated_at FROM playlists WHERE device_id = ? ORDER BY created_at DESC`, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playlist{}
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ids, err := s.playlistTrackIDs(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].TrackIDs = ids
	}
	return out, nil
}

func (s *Store) Playlist(ctx context.Context, device, id string) (*Playlist, error) {
	var p Playlist
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at, updated_at FROM playlists WHERE id = ? AND device_id = ?`, id, device).
		Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.TrackIDs, err = s.playlistTrackIDs(ctx, id)
	return &p, err
}

func (s *Store) playlistTrackIDs(ctx context.Context, id string) ([]string, error) {
	return s.strings(ctx, `SELECT track_id FROM playlist_tracks WHERE playlist_id = ? ORDER BY position`, id)
}

func (s *Store) RenamePlaylist(ctx context.Context, device, id, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE playlists SET name = ?, updated_at = ? WHERE id = ? AND device_id = ?`, name, s.ms(), id, device)
	return affected(res, err)
}

func (s *Store) DeletePlaylist(ctx context.Context, device, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM playlists WHERE id = ? AND device_id = ?`, id, device)
	return affected(res, err)
}

// SetPlaylistTracks replaces the ordered track list in one transaction.
func (s *Store) SetPlaylistTracks(ctx context.Context, device, id string, trackIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = ? WHERE id = ? AND device_id = ?`, s.ms(), id, device)
	if err := affected(res, err); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE playlist_id = ?`, id); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO playlist_tracks(playlist_id, position, track_id, added_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, t := range trackIDs {
		if _, err := stmt.ExecContext(ctx, id, i, t, s.ms()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- history ----

type HistoryEvent struct {
	TrackID  string `json:"trackId"`
	PlayedAt int64  `json:"playedAt"` // unix ms
	MsPlayed int64  `json:"msPlayed"`
	Skipped  bool   `json:"skipped"`
	Context  string `json:"context,omitempty"`
}

const historyRetention = 2000

func (s *Store) AddHistory(ctx context.Context, device string, events []HistoryEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO history(device_id, track_id, played_at, ms_played, skipped, context) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range events {
		skipped := 0
		if e.Skipped {
			skipped = 1
		}
		if _, err := stmt.ExecContext(ctx, device, e.TrackID, e.PlayedAt, e.MsPlayed, skipped, e.Context); err != nil {
			return err
		}
	}
	// Bounded retention keeps history private-by-default and small.
	if _, err := tx.ExecContext(ctx, `DELETE FROM history WHERE device_id = ? AND id NOT IN (
		SELECT id FROM history WHERE device_id = ? ORDER BY played_at DESC, id DESC LIMIT ?)`, device, device, historyRetention); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) History(ctx context.Context, device string, limit int) ([]HistoryEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT track_id, played_at, ms_played, skipped, context FROM history
		WHERE device_id = ? ORDER BY played_at DESC, id DESC LIMIT ?`, device, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryEvent{}
	for rows.Next() {
		var e HistoryEvent
		var skipped int
		if err := rows.Scan(&e.TrackID, &e.PlayedAt, &e.MsPlayed, &skipped, &e.Context); err != nil {
			return nil, err
		}
		e.Skipped = skipped == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// RecentTracks returns distinct track IDs, most recently played first.
func (s *Store) RecentTracks(ctx context.Context, device string, limit int) ([]string, error) {
	return s.strings(ctx, `SELECT track_id FROM history WHERE device_id = ? GROUP BY track_id ORDER BY max(played_at) DESC LIMIT ?`, device, limit)
}

// SkipCounts returns how often each recently played track was skipped.
func (s *Store) SkipCounts(ctx context.Context, device string, sinceMs int64) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT track_id, sum(skipped) FROM history WHERE device_id = ? AND played_at >= ? GROUP BY track_id HAVING sum(skipped) > 0`, device, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (s *Store) ClearHistory(ctx context.Context, device string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM history WHERE device_id = ?`, device)
	return err
}

// ---- preferences ----

func (s *Store) Preferences(ctx context.Context, device string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM preferences WHERE device_id = ?`, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetPreferences(ctx context.Context, device string, prefs map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range prefs {
		if v == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM preferences WHERE device_id = ? AND key = ?`, device, k); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO preferences(device_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT(device_id, key) DO UPDATE SET value = excluded.value`, device, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- downloads ----

type Download struct {
	TrackID    string `json:"trackId"`
	LocalID    string `json:"localId"`
	Status     string `json:"status"`
	BytesTotal int64  `json:"bytesTotal"`
	BytesDone  int64  `json:"bytesDone"`
	SHA256     string `json:"sha256,omitempty"`
	Error      string `json:"error,omitempty"`
	UpdatedAt  int64  `json:"updatedAt"`
}

func (s *Store) Download(ctx context.Context, device, trackID string) (*Download, error) {
	var d Download
	err := s.db.QueryRowContext(ctx, `SELECT track_id, local_id, status, bytes_total, bytes_done, sha256, error, updated_at
		FROM downloads WHERE device_id = ? AND track_id = ?`, device, trackID).
		Scan(&d.TrackID, &d.LocalID, &d.Status, &d.BytesTotal, &d.BytesDone, &d.SHA256, &d.Error, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (s *Store) Downloads(ctx context.Context, device string) ([]Download, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT track_id, local_id, status, bytes_total, bytes_done, sha256, error, updated_at
		FROM downloads WHERE device_id = ? ORDER BY updated_at DESC`, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Download{}
	for rows.Next() {
		var d Download
		if err := rows.Scan(&d.TrackID, &d.LocalID, &d.Status, &d.BytesTotal, &d.BytesDone, &d.SHA256, &d.Error, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutDownload upserts a download row. expectStatus implements optimistic
// concurrency: the write only applies if the current status still matches
// (use "" when the row must not exist yet).
func (s *Store) PutDownload(ctx context.Context, device string, d Download, expectStatus string) error {
	d.UpdatedAt = s.ms()
	if expectStatus == "" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO downloads(device_id, track_id, local_id, status, bytes_total, bytes_done, sha256, error, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, device, d.TrackID, d.LocalID, d.Status, d.BytesTotal, d.BytesDone, d.SHA256, d.Error, d.UpdatedAt)
		if err != nil && strings.Contains(err.Error(), "UNIQUE") {
			return ErrConflict
		}
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE downloads SET status = ?, bytes_total = ?, bytes_done = ?, sha256 = ?, error = ?, updated_at = ?
		WHERE device_id = ? AND track_id = ? AND status = ?`, d.Status, d.BytesTotal, d.BytesDone, d.SHA256, d.Error, d.UpdatedAt, device, d.TrackID, expectStatus)
	if err := affected(res, err); errors.Is(err, ErrNotFound) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	return nil
}

var ErrConflict = errors.New("conflict")

func (s *Store) DeleteDownload(ctx context.Context, device, trackID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM downloads WHERE device_id = ? AND track_id = ?`, device, trackID)
	return affected(res, err)
}

// ---- queue state ----

func (s *Store) SaveQueue(ctx context.Context, device string, data []byte, revision int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO queue_state(device_id, data, revision, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET data = excluded.data, revision = excluded.revision, updated_at = excluded.updated_at`,
		device, string(data), revision, s.ms())
	return err
}

func (s *Store) LoadQueue(ctx context.Context, device string) ([]byte, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM queue_state WHERE device_id = ?`, device).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return []byte(data), err
}

// ---- helpers ----

func (s *Store) strings(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func anys(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
