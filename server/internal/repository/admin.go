package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// ---- app settings ----

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
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

// SetSettings upserts every key in one transaction; an empty value deletes
// the key.
func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if v == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM app_settings WHERE key = ?`, k); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, k, v, s.ms()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- popups ----

type Popup struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	ButtonText  string `json:"buttonText"`
	ButtonURL   string `json:"buttonUrl"`
	Dismissible bool   `json:"dismissible"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

const popupCols = `id, kind, title, message, button_text, button_url, dismissible, created_at, updated_at`

func scanPopup(sc interface{ Scan(...any) error }) (Popup, error) {
	var p Popup
	err := sc.Scan(&p.ID, &p.Kind, &p.Title, &p.Message, &p.ButtonText, &p.ButtonURL, &p.Dismissible, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) Popups(ctx context.Context) ([]Popup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+popupCols+` FROM popups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Popup{}
	for rows.Next() {
		p, err := scanPopup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Popup(ctx context.Context, id int64) (*Popup, error) {
	p, err := scanPopup(s.db.QueryRowContext(ctx, `SELECT `+popupCols+` FROM popups WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreatePopup(ctx context.Context, p Popup) (*Popup, error) {
	now := s.ms()
	res, err := s.db.ExecContext(ctx, `INSERT INTO popups(kind, title, message, button_text, button_url, dismissible, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.Kind, p.Title, p.Message, p.ButtonText, p.ButtonURL, p.Dismissible, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Popup(ctx, id)
}

func (s *Store) UpdatePopup(ctx context.Context, p Popup) (*Popup, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE popups SET kind = ?, title = ?, message = ?, button_text = ?, button_url = ?,
		dismissible = ?, updated_at = ? WHERE id = ?`, p.Kind, p.Title, p.Message, p.ButtonText, p.ButtonURL, p.Dismissible, s.ms(), p.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.Popup(ctx, p.ID)
}

func (s *Store) DeletePopup(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM popups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FormatID is the string form of a popup ID used in settings.
func FormatID(id int64) string { return strconv.FormatInt(id, 10) }

// NowMs is the store clock in Unix milliseconds.
func (s *Store) NowMs() int64 { return s.ms() }
