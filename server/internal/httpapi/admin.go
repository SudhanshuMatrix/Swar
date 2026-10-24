package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"swar/internal/catalog"
	"swar/internal/repository"
)

// Settings keys in app_settings.
const (
	keyMaintenance        = "maintenance"
	keyMaintenanceMessage = "maintenance_message"
	keyActivePopup        = "active_popup"
	keyActivePopupAt      = "active_popup_at"
)

const defaultMaintenanceMessage = "Swar is down for maintenance. Please check back soon."

var popupKinds = map[string]bool{"update": true, "support": true, "info": true}

// appStatus is what every client needs to know about the server: whether
// it is in maintenance and which popup (if any) to show. It is cached in
// memory because it goes out as headers on every response.
type appStatus struct {
	Maintenance maintenance       `json:"maintenance"`
	Popup       *repository.Popup `json:"popup"`
	// PopupKey changes whenever a popup is activated or edited, so clients
	// show it again even if they dismissed an earlier version.
	PopupKey string `json:"popupKey"`
}

type maintenance struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}

type statusCache struct {
	mu sync.RWMutex
	st appStatus
}

func (c *statusCache) get() appStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.st
}

func (c *statusCache) set(st appStatus) {
	c.mu.Lock()
	c.st = st
	c.mu.Unlock()
}

// reloadStatus rebuilds the cached status from the database.
func (s *Server) reloadStatus(ctx context.Context) error {
	kv, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	st := appStatus{Maintenance: maintenance{Enabled: kv[keyMaintenance] == "on", Message: kv[keyMaintenanceMessage]}}
	if st.Maintenance.Message == "" {
		st.Maintenance.Message = defaultMaintenanceMessage
	}
	if id, err := strconv.ParseInt(kv[keyActivePopup], 10, 64); err == nil {
		p, err := s.store.Popup(ctx, id)
		switch {
		case err == nil:
			st.Popup = p
			st.PopupKey = kv[keyActivePopup] + "-" + kv[keyActivePopupAt] + "-" + strconv.FormatInt(p.UpdatedAt, 10)
		case !errors.Is(err, repository.ErrNotFound):
			return err
		}
	}
	s.status.set(st)
	return nil
}

// appState adds the maintenance and popup headers to every response and,
// during maintenance, turns away everything except health checks, the
// status endpoint and the admin API.
func (s *Server) appState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := s.stampStatus(w)
		if st.Maintenance.Enabled && !maintenanceExempt(r.URL.Path) {
			w.Header().Set("Retry-After", "60")
			fail(w, http.StatusServiceUnavailable, "maintenance", st.Maintenance.Message)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func maintenanceExempt(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/health", "/metrics", "/v1/status":
		return true
	}
	return strings.HasPrefix(path, "/v1/admin/")
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	ok(w, s.status.get())
}

// requireAdmin checks HTTP Basic credentials against ADMIN_USERNAME and
// ADMIN_PASSWORD. Wrong guesses drain the caller's rate-limit bucket so
// the password cannot be brute-forced quickly.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		if !s.cfg.AdminEnabled() {
			fail(w, http.StatusNotFound, "admin_disabled", "Admin API is off; set ADMIN_USERNAME and ADMIN_PASSWORD on the server")
			return
		}
		user, pass, found := r.BasicAuth()
		if !found || !secretEqual(user, s.cfg.AdminUsername) || !secretEqual(pass, s.cfg.AdminPassword) {
			s.limiter.allow(s.clientIP(r), 20)
			s.log.Warn("admin login failed", "ip", s.clientIP(r))
			w.Header().Set("WWW-Authenticate", `Basic realm="swar-admin"`)
			fail(w, http.StatusUnauthorized, "unauthorized", "Wrong admin username or password")
			return
		}
		h(w, r)
	}
}

// secretEqual compares in constant time; hashing first hides the length.
func secretEqual(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// ---- maintenance ----

func (s *Server) handleAdminGetMaintenance(w http.ResponseWriter, r *http.Request) {
	ok(w, s.status.get().Maintenance)
}

func (s *Server) handleAdminSetMaintenance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool   `json:"enabled"`
		Message *string `json:"message"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if body.Enabled == nil {
		s.writeErr(w, r, catalog.Invalid(`"enabled" (true or false) is required`))
		return
	}
	kv := map[string]string{keyMaintenance: "off"}
	if *body.Enabled {
		kv[keyMaintenance] = "on"
	}
	if body.Message != nil {
		msg := strings.TrimSpace(*body.Message)
		if len(msg) > 500 {
			s.writeErr(w, r, catalog.Invalid("message must be at most 500 characters"))
			return
		}
		kv[keyMaintenanceMessage] = msg
	}
	if err := s.saveSettings(r.Context(), kv); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.log.Info("maintenance changed", "enabled", *body.Enabled)
	// The headers were set before the change; send the new state.
	s.stampStatus(w)
	ok(w, s.status.get().Maintenance)
}

// ---- caches ----

// handleAdminClearCache empties every in-memory cache: catalog responses
// and playback descriptors, Deep Search results, YouTube matches and the
// device check cache. The server keeps no Redis or file cache. Nothing is
// lost: the next requests load fresh data from the providers and the
// database.
func (s *Server) handleAdminClearCache(w http.ResponseWriter, r *http.Request) {
	cat := s.cat.ClearCache()
	cleared := map[string]int{
		"catalog":        cat.Entries,
		"playback":       s.pb.ClearCache(), // 0 when it shares the catalog's cache
		"youtubeMatches": cat.YouTubeMatches,
		"deepSearch":     s.deep.ClearCache(),
		"devices":        s.devCache.Clear(),
	}
	total := 0
	for _, n := range cleared {
		total += n
	}
	s.log.Info("caches cleared", "entries", total, "ip", s.clientIP(r))
	ok(w, map[string]any{"cleared": cleared, "total": total})
}

// ---- popups ----

type popupBody struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	ButtonText  string `json:"buttonText"`
	ButtonURL   string `json:"buttonUrl"`
	Dismissible *bool  `json:"dismissible"`
	Activate    bool   `json:"activate"`
}

func (b popupBody) toPopup() (repository.Popup, error) {
	p := repository.Popup{
		Kind: strings.ToLower(strings.TrimSpace(b.Kind)), Title: strings.TrimSpace(b.Title),
		Message: strings.TrimSpace(b.Message), ButtonText: strings.TrimSpace(b.ButtonText),
		ButtonURL: strings.TrimSpace(b.ButtonURL), Dismissible: true,
	}
	if b.Dismissible != nil {
		p.Dismissible = *b.Dismissible
	}
	if p.Kind == "" {
		p.Kind = "info"
	}
	switch {
	case !popupKinds[p.Kind]:
		return p, catalog.Invalid("kind must be update, support or info")
	case p.Title == "" || len(p.Title) > 120:
		return p, catalog.Invalid("title is required (at most 120 characters)")
	case p.Message == "" || len(p.Message) > 2000:
		return p, catalog.Invalid("message is required (at most 2000 characters)")
	case len(p.ButtonText) > 40:
		return p, catalog.Invalid("buttonText must be at most 40 characters")
	case len(p.ButtonURL) > 500:
		return p, catalog.Invalid("buttonUrl must be at most 500 characters")
	}
	if p.ButtonURL != "" {
		u, err := url.Parse(p.ButtonURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return p, catalog.Invalid("buttonUrl must be an http or https link")
		}
	}
	return p, nil
}

func (s *Server) handleAdminListPopups(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Popups(r.Context())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	var active int64
	if st := s.status.get(); st.Popup != nil {
		active = st.Popup.ID
	}
	ok(w, map[string]any{"popups": list, "activeId": active})
}

func (s *Server) handleAdminGetPopup(w http.ResponseWriter, r *http.Request) {
	id, err := popupID(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := s.store.Popup(r.Context(), id)
	if err != nil {
		s.writePopupErr(w, r, err)
		return
	}
	ok(w, p)
}

func (s *Server) handleAdminCreatePopup(w http.ResponseWriter, r *http.Request) {
	var body popupBody
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := body.toPopup()
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	created, err := s.store.CreatePopup(r.Context(), p)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if body.Activate {
		if err := s.activatePopup(r.Context(), created.ID); err != nil {
			s.writeErr(w, r, err)
			return
		}
		s.stampStatus(w)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "data": created})
}

func (s *Server) handleAdminUpdatePopup(w http.ResponseWriter, r *http.Request) {
	id, err := popupID(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	var body popupBody
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	p, err := body.toPopup()
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	p.ID = id
	updated, err := s.store.UpdatePopup(r.Context(), p)
	if err != nil {
		s.writePopupErr(w, r, err)
		return
	}
	if body.Activate {
		err = s.activatePopup(r.Context(), id)
	} else {
		err = s.reloadStatus(r.Context())
	}
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.stampStatus(w)
	ok(w, updated)
}

func (s *Server) handleAdminDeletePopup(w http.ResponseWriter, r *http.Request) {
	id, err := popupID(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if st := s.status.get(); st.Popup != nil && st.Popup.ID == id {
		if err := s.saveSettings(r.Context(), map[string]string{keyActivePopup: "", keyActivePopupAt: ""}); err != nil {
			s.writeErr(w, r, err)
			return
		}
	}
	if err := s.store.DeletePopup(r.Context(), id); err != nil {
		s.writePopupErr(w, r, err)
		return
	}
	if err := s.reloadStatus(r.Context()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.stampStatus(w)
	ok(w, map[string]any{"deleted": id})
}

func (s *Server) handleAdminGetActivePopup(w http.ResponseWriter, r *http.Request) {
	st := s.status.get()
	ok(w, map[string]any{"active": st.Popup != nil, "popup": st.Popup})
}

// handleAdminActivatePopup shows the popup with the given ID to every
// client, replacing whichever popup was active.
func (s *Server) handleAdminActivatePopup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID int64 `json:"id"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if body.ID <= 0 {
		s.writeErr(w, r, catalog.Invalid(`"id" of a popup is required`))
		return
	}
	if err := s.activatePopup(r.Context(), body.ID); err != nil {
		s.writePopupErr(w, r, err)
		return
	}
	s.stampStatus(w)
	st := s.status.get()
	ok(w, map[string]any{"active": true, "popup": st.Popup})
}

func (s *Server) handleAdminDeactivatePopup(w http.ResponseWriter, r *http.Request) {
	if err := s.saveSettings(r.Context(), map[string]string{keyActivePopup: "", keyActivePopupAt: ""}); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.stampStatus(w)
	ok(w, map[string]any{"active": false, "popup": nil})
}

func (s *Server) activatePopup(ctx context.Context, id int64) error {
	if _, err := s.store.Popup(ctx, id); err != nil {
		return err
	}
	return s.saveSettings(ctx, map[string]string{
		keyActivePopup:   repository.FormatID(id),
		keyActivePopupAt: strconv.FormatInt(s.store.NowMs(), 10),
	})
}

func (s *Server) saveSettings(ctx context.Context, kv map[string]string) error {
	if err := s.store.SetSettings(ctx, kv); err != nil {
		return err
	}
	return s.reloadStatus(ctx)
}

// stampStatus sets the status headers from the current state. Admin
// handlers call it again after a change so the response reports the new
// state.
func (s *Server) stampStatus(w http.ResponseWriter) appStatus {
	st := s.status.get()
	if st.Maintenance.Enabled {
		w.Header().Set("X-Swar-Maintenance", "on")
	} else {
		w.Header().Set("X-Swar-Maintenance", "off")
	}
	if st.PopupKey != "" {
		w.Header().Set("X-Swar-Popup", st.PopupKey)
	} else {
		w.Header().Set("X-Swar-Popup", "none")
	}
	return st
}

func (s *Server) writePopupErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found", "No popup with that id")
		return
	}
	s.writeErr(w, r, err)
}

func popupID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, catalog.Invalid("popup id must be a positive number")
	}
	return id, nil
}
