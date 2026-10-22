package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

var adminVars = map[string]string{"ADMIN_USERNAME": "boss", "ADMIN_PASSWORD": "correct horse"}

// admin sends an admin request with HTTP Basic credentials and returns the
// status, decoded body and response headers.
func (e *env) admin(t *testing.T, method, path, user, pass string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m, res.Header
}

func (e *env) get(t *testing.T, path string) (int, http.Header) {
	t.Helper()
	res, err := http.Get(e.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode, res.Header
}

func TestAdminDisabledWithoutCredentials(t *testing.T) {
	e := newEnv(t, setup{})
	code, body, _ := e.admin(t, "GET", "/v1/admin/maintenance", "boss", "correct horse", nil)
	if code != http.StatusNotFound || errCode(body) != "admin_disabled" {
		t.Fatalf("got %d %v", code, body)
	}
}

func TestAdminRejectsWrongPassword(t *testing.T) {
	e := newEnv(t, setup{env: adminVars})
	for _, c := range [][2]string{{"", ""}, {"boss", "wrong"}, {"other", "correct horse"}} {
		code, _, h := e.admin(t, "PUT", "/v1/admin/maintenance", c[0], c[1], map[string]any{"enabled": true})
		if code != http.StatusUnauthorized || h.Get("WWW-Authenticate") == "" {
			t.Fatalf("%v: got %d", c, code)
		}
	}
	if _, h := e.get(t, "/healthz"); h.Get("X-Swar-Maintenance") != "off" {
		t.Fatal("maintenance must stay off after failed logins")
	}
}

func TestMaintenanceMode(t *testing.T) {
	e := newEnv(t, setup{env: adminVars})
	if _, h := e.get(t, "/v1/search?q=x"); h.Get("X-Swar-Maintenance") != "off" || h.Get("X-Swar-Popup") != "none" {
		t.Fatalf("headers: %v", h)
	}

	code, body, h := e.admin(t, "PUT", "/v1/admin/maintenance", "boss", "correct horse",
		map[string]any{"enabled": true, "message": "Back at 6 pm"})
	if code != 200 || h.Get("X-Swar-Maintenance") != "on" {
		t.Fatalf("enable: %d %v", code, body)
	}

	code, body = e.do(t, "GET", "/v1/search?q=x", "", nil)
	if code != http.StatusServiceUnavailable || errCode(body) != "maintenance" || body["message"] != "Back at 6 pm" {
		t.Fatalf("search during maintenance: %d %v", code, body)
	}
	if code, _ := e.get(t, "/v1/playback/abc"); code != http.StatusServiceUnavailable {
		t.Fatalf("playback during maintenance: %d", code)
	}
	for _, p := range []string{"/healthz", "/health", "/v1/status"} {
		if code, h := e.get(t, p); code != 200 || h.Get("X-Swar-Maintenance") != "on" {
			t.Fatalf("%s: %d %v", p, code, h)
		}
	}
	code, body = e.do(t, "GET", "/v1/status", "", nil)
	m := body["data"].(map[string]any)["maintenance"].(map[string]any)
	if code != 200 || m["enabled"] != true || m["message"] != "Back at 6 pm" {
		t.Fatalf("status: %v", body)
	}

	code, _, h = e.admin(t, "PUT", "/v1/admin/maintenance", "boss", "correct horse", map[string]any{"enabled": false})
	if code != 200 || h.Get("X-Swar-Maintenance") != "off" {
		t.Fatalf("disable: %d", code)
	}
	if code, _ := e.do(t, "GET", "/v1/search?q=x", "", nil); code == http.StatusServiceUnavailable {
		t.Fatal("search still blocked after maintenance was turned off")
	}
}

func TestPopups(t *testing.T) {
	e := newEnv(t, setup{env: adminVars})
	admin := func(method, path string, body any) (int, map[string]any, http.Header) {
		return e.admin(t, method, path, "boss", "correct horse", body)
	}

	code, body, _ := admin("POST", "/v1/admin/popups", map[string]any{"kind": "nope", "title": "x", "message": "y"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d %v", code, body)
	}
	code, body, _ = admin("POST", "/v1/admin/popups", map[string]any{"kind": "update", "title": "Update", "message": "m", "buttonUrl": "javascript:x"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad url: %d %v", code, body)
	}

	code, body, _ = admin("POST", "/v1/admin/popups", map[string]any{
		"kind": "update", "title": "New version", "message": "Get 1.2.4", "buttonText": "Update",
		"buttonUrl": "https://github.com/SudhanshuMatrix/Swar/releases/latest", "dismissible": false,
	})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, body)
	}
	first := body["data"].(map[string]any)["id"].(float64)
	code, body, _ = admin("POST", "/v1/admin/popups", map[string]any{"kind": "support", "title": "Support us", "message": "Buy us a coffee"})
	second := body["data"].(map[string]any)["id"].(float64)

	code, body, _ = admin("GET", "/v1/admin/popups", nil)
	if n := len(body["data"].(map[string]any)["popups"].([]any)); code != 200 || n != 2 {
		t.Fatalf("list: %d %v", code, body)
	}

	// Activate the first, then replace it with the second.
	code, _, h := admin("PUT", "/v1/admin/popups/active", map[string]any{"id": first})
	key1 := h.Get("X-Swar-Popup")
	if code != 200 || key1 == "none" {
		t.Fatalf("activate: %d %v", code, h)
	}
	code, body = e.do(t, "GET", "/v1/status", "", nil)
	p := body["data"].(map[string]any)["popup"].(map[string]any)
	if p["title"] != "New version" || p["dismissible"] != false {
		t.Fatalf("status popup: %v", body)
	}
	_, _, h = admin("PUT", "/v1/admin/popups/active", map[string]any{"id": second})
	if h.Get("X-Swar-Popup") == key1 {
		t.Fatal("popup key must change when another popup is activated")
	}
	_, body = e.do(t, "GET", "/v1/status", "", nil)
	if body["data"].(map[string]any)["popup"].(map[string]any)["title"] != "Support us" {
		t.Fatalf("replace: %v", body)
	}

	// Editing the active popup changes the key so clients show it again.
	_, h2 := e.get(t, "/v1/status")
	code, _, h = admin("PUT", "/v1/admin/popups/2", map[string]any{"kind": "support", "title": "Support Swar", "message": "Thanks"})
	if code != 200 || h.Get("X-Swar-Popup") == h2.Get("X-Swar-Popup") {
		t.Fatalf("edit: %d", code)
	}

	if code, _, _ := admin("PUT", "/v1/admin/popups/active", map[string]any{"id": 99}); code != http.StatusNotFound {
		t.Fatalf("activate missing: %d", code)
	}

	code, _, h = admin("DELETE", "/v1/admin/popups/active", nil)
	if code != 200 || h.Get("X-Swar-Popup") != "none" {
		t.Fatalf("deactivate: %d %v", code, h)
	}
	_, body = e.do(t, "GET", "/v1/status", "", nil)
	if body["data"].(map[string]any)["popup"] != nil {
		t.Fatalf("popup still active: %v", body)
	}

	// Deleting the active popup also deactivates it.
	admin("PUT", "/v1/admin/popups/active", map[string]any{"id": first})
	if code, _, h = admin("DELETE", "/v1/admin/popups/1", nil); code != 200 || h.Get("X-Swar-Popup") != "none" {
		t.Fatalf("delete active: %d %v", code, h)
	}
	if code, _, _ := admin("GET", "/v1/admin/popups/1", nil); code != http.StatusNotFound {
		t.Fatalf("deleted popup still there: %d", code)
	}
}
