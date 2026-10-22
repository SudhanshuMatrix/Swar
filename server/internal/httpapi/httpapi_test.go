package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"swar/internal/auth"
	"swar/internal/cache"
	"swar/internal/config"
	"swar/internal/downloads"
	"swar/internal/jam"
	"swar/internal/library"
	"swar/internal/observability"
	"swar/internal/playback"
	"swar/internal/providers/deepsearch"
	"swar/internal/radio"
	"swar/internal/recommendations"
	"swar/internal/search"
	"swar/internal/testkit"
)

type env struct {
	srv  *httptest.Server
	fake *testkit.Fake
	deep *[]string
}

type setup struct {
	env     map[string]string
	media   bool
	deepOut []byte
}

func newEnv(t *testing.T, o setup) *env {
	t.Helper()
	vars := map[string]string{"SWAR_RATE_LIMIT_RPS": "1000", "SWAR_RATE_LIMIT_BURST": "1000", "SWAR_MAX_BODY_BYTES": "4096"}
	for k, v := range o.env {
		vars[k] = v
	}
	cfg, err := config.LoadFrom(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := observability.NewMetrics()
	cat, fake := testkit.Catalog(t, testkit.Options{MediaURLs: o.media})
	store := testkit.Store(t)
	var deepArgs []string
	out := o.deepOut
	deep := deepsearch.New(deepsearch.Options{MaxResults: 5, Runner: func(ctx context.Context, args []string, max int64) ([]byte, error) {
		deepArgs = args
		return out, nil
	}})
	cat.RegisterSource("yt", deep)
	resolver := playback.NewResolver(cat, cache.New(100), metrics)
	lib := library.New(store, cat)
	rec := recommendations.NewService(cat, lib)
	s := New(Deps{
		Config: cfg, Log: log, Metrics: metrics, Catalog: cat, Search: search.New(cat), DeepSearch: deep,
		Playback: resolver, Recommendations: rec, Radio: radio.NewManager(cat, rec, lib, time.Hour), Library: lib,
		Downloads: downloads.New(store, cat, resolver),
		Jam:       jam.NewManager(jam.Options{MaxParticipants: 5, MaxQueue: 50, IdleTTL: time.Hour, MaxLifetime: time.Hour}, cat, metrics),
		Store:     store, Tokens: auth.NewTokens(cfg.TokenSecret, cfg.TokenTTL),
	})
	hs := httptest.NewServer(s)
	t.Cleanup(hs.Close)
	return &env{srv: hs, fake: fake, deep: &deepArgs}
}

func (e *env) do(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		buf, _ := json.Marshal(b)
		rd = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 && json.Unmarshal(raw, &m) != nil {
		m = map[string]any{"_raw": string(raw)}
	}
	return res.StatusCode, m
}

func (e *env) device(t *testing.T) string {
	t.Helper()
	code, body := e.do(t, "POST", "/v1/auth/device", "", nil)
	if code != 201 {
		t.Fatalf("register: %d %v", code, body)
	}
	return body["data"].(map[string]any)["token"].(string)
}

func errCode(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	c, _ := m["code"].(string)
	return c
}

func TestLegacyFlutterContract(t *testing.T) {
	e := newEnv(t, setup{})
	code, body := e.do(t, "GET", "/search?q=kesariya&limit=5", "", nil)
	if code != 200 {
		t.Fatalf("search: %d %v", code, body)
	}
	results := body["results"].([]any)
	first := results[0].(map[string]any)
	for _, k := range []string{"id", "title", "artist", "thumbnail", "duration"} {
		if _, ok := first[k]; !ok {
			t.Errorf("legacy result missing %q: %v", k, first)
		}
	}
	if first["id"] != "saavn:s1" || first["artist"] != "Arijit Singh" {
		t.Errorf("first result: %v", first)
	}
	if code, body := e.do(t, "GET", "/health", "", nil); code != 200 || body["status"] != "ok" {
		t.Fatalf("health: %d %v", code, body)
	}
	// Media URLs are disabled by default: an honest 403, not a fake URL.
	code, body = e.do(t, "GET", "/track/saavn:s1", "", nil)
	if code != 403 || body["audio_url"] != nil {
		t.Fatalf("track with media disabled: %d %v", code, body)
	}
	if code, _ := e.do(t, "GET", "/search?q=", "", nil); code != 400 {
		t.Fatalf("empty query: %d", code)
	}
}

func TestLegacyTrackWithMediaEnabled(t *testing.T) {
	e := newEnv(t, setup{media: true})
	code, body := e.do(t, "GET", "/track/saavn:s1", "", nil)
	if code != 200 {
		t.Fatalf("track: %d %v", code, body)
	}
	if u, _ := body["audio_url"].(string); !strings.HasPrefix(u, "https://aac.saavncdn.com/s1_") {
		t.Fatalf("audio_url = %v", body["audio_url"])
	}
	code, body = e.do(t, "GET", "/v1/playback/saavn:s1", "", nil)
	data := body["data"].(map[string]any)
	if code != 200 || data["available"] != true || len(data["sources"].([]any)) == 0 {
		t.Fatalf("descriptor: %d %v", code, body)
	}
}

func TestV1SearchAndCompat(t *testing.T) {
	e := newEnv(t, setup{})
	code, body := e.do(t, "GET", "/v1/search?q=kesariya&type=songs", "", nil)
	if code != 200 || body["success"] != true {
		t.Fatalf("v1 search: %d %v", code, body)
	}
	songs := body["data"].(map[string]any)["songs"].(map[string]any)["results"].([]any)
	if len(songs) != 2 {
		t.Fatalf("dedupe: want 2 songs, got %d", len(songs))
	}
	if code, body := e.do(t, "GET", "/v1/search?q=x&type=bogus", "", nil); code != 400 || errCode(body) != "invalid_request" {
		t.Fatalf("bad type: %d %v", code, body)
	}
	if code, body := e.do(t, "GET", "/v1/tracks/saavn:missing", "", nil); code != 404 {
		t.Fatalf("missing track: %d %v", code, body)
	}
	if code, _ := e.do(t, "GET", "/nope", "", nil); code != 404 {
		t.Fatalf("unknown route: %d", code)
	}
}

func TestAuthAndLimits(t *testing.T) {
	e := newEnv(t, setup{env: map[string]string{"SWAR_RATE_LIMIT_RPS": "1", "SWAR_RATE_LIMIT_BURST": "5"}})
	if code, body := e.do(t, "GET", "/v1/library/likes", "", nil); code != 401 || errCode(body) != "unauthorized" {
		t.Fatalf("no token: %d %v", code, body)
	}
	if code, _ := e.do(t, "GET", "/v1/library/likes", "v1.forged.token", nil); code != 401 {
		t.Fatalf("forged token: %d", code)
	}
	tok := e.device(t)
	big := `{"name":"` + strings.Repeat("x", 5000) + `"}`
	if code, body := e.do(t, "POST", "/v1/library/playlists", tok, big); code != 413 {
		t.Fatalf("oversized body: %d %v", code, body)
	}
	if code, _ := e.do(t, "POST", "/v1/library/playlists", tok, `{"name":"a","evil":1}`); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}
	limited := false
	for i := 0; i < 10; i++ {
		if code, body := e.do(t, "GET", "/v1/browse/trending", "", nil); code == 429 {
			limited = errCode(body) == "rate_limited"
			break
		}
	}
	if !limited {
		t.Fatal("rate limit never triggered")
	}
	// Health checks are never rate limited.
	if code, _ := e.do(t, "GET", "/healthz", "", nil); code != 200 {
		t.Fatalf("healthz limited: %d", code)
	}
}

func TestLibraryFlow(t *testing.T) {
	e := newEnv(t, setup{})
	tok := e.device(t)
	if code, body := e.do(t, "PUT", "/v1/library/likes/saavn:s1", tok, nil); code != 200 {
		t.Fatalf("like: %d %v", code, body)
	}
	code, body := e.do(t, "GET", "/v1/library/likes", tok, nil)
	if code != 200 || !strings.Contains(mustJSON(body), "saavn:s1") {
		t.Fatalf("likes: %d %v", code, body)
	}
	// Another device does not see it.
	other := e.device(t)
	_, body = e.do(t, "GET", "/v1/library/likes", other, nil)
	if strings.Contains(mustJSON(body), "saavn:s1") {
		t.Fatal("likes leaked across devices")
	}
	// Deleting the device revokes its token.
	if code, _ := e.do(t, "DELETE", "/v1/auth/device", tok, nil); code != 200 {
		t.Fatalf("delete device: %d", code)
	}
	if code, _ := e.do(t, "GET", "/v1/library/likes", tok, nil); code != 401 {
		t.Fatalf("deleted device still authorized: %d", code)
	}
}

func TestDeepSearchEndpoint(t *testing.T) {
	out, _ := json.Marshal(map[string]any{"entries": []any{map[string]any{"id": "AAAAAAAAAAA", "title": "Rare Song", "channel": "Someone"}}})
	e := newEnv(t, setup{deepOut: out})
	if code, _ := e.do(t, "GET", "/v1/search/deep?query=x", "", nil); code == 200 {
		t.Fatalf("deep search must be POST-only, got %d", code)
	}
	code, body := e.do(t, "POST", "/v1/search/deep", "", map[string]any{"query": "rare song"})
	if code != 200 {
		t.Fatalf("deep: %d %v", code, body)
	}
	if !strings.Contains(mustJSON(body), "yt:AAAAAAAAAAA") || !strings.Contains(mustJSON(body), "metadata_only") {
		t.Fatalf("deep results: %v", body)
	}
	if args := *e.deep; args[len(args)-1] != "ytsearch5:rare song" {
		t.Fatalf("args: %v", args)
	}
	if code, _ := e.do(t, "POST", "/v1/search/deep", "", map[string]any{"query": "http://169.254.169.254/"}); code != 400 {
		t.Fatalf("ssrf-like input: %d", code)
	}
	disabled := newEnv(t, setup{env: map[string]string{"DEEPSEARCH_ENABLED": "false"}})
	if code, _ := disabled.do(t, "POST", "/v1/search/deep", "", map[string]any{"query": "x y"}); code != 403 {
		t.Fatalf("disabled deep search: %d", code)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

type wsClient struct {
	t *testing.T
	c *websocket.Conn
}

func (e *env) dial(t *testing.T, jamID, tok string) (*wsClient, *http.Response, error) {
	u := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/v1/jams/" + jamID + "/ws?access_token=" + tok
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		return nil, res, err
	}
	t.Cleanup(func() { c.CloseNow() })
	return &wsClient{t: t, c: c}, res, nil
}

// next reads messages until one of type typ arrives.
func (w *wsClient) next(typ string) map[string]any {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, b, err := w.c.Read(ctx)
		if err != nil {
			w.t.Fatalf("waiting for %q: %v", typ, err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if m["type"] == typ {
			return m
		}
	}
}

func (w *wsClient) send(v any) {
	w.t.Helper()
	b, _ := json.Marshal(v)
	if err := w.c.Write(context.Background(), websocket.MessageText, b); err != nil {
		w.t.Fatal(err)
	}
}

func TestJamOverWebSocket(t *testing.T) {
	e := newEnv(t, setup{})
	host, guest := e.device(t), e.device(t)
	code, body := e.do(t, "POST", "/v1/jams", host, map[string]any{"displayName": "Asha", "settings": map[string]any{"allowParticipantAdd": true}})
	if code != 201 {
		t.Fatalf("create: %d %v", code, body)
	}
	jamData := body["data"].(map[string]any)
	id, jcode := jamData["id"].(string), jamData["code"].(string)
	if code, body := e.do(t, "POST", "/v1/jams/join", guest, map[string]any{"code": "WRONG1"}); code != 404 || errCode(body) != "jam_not_found" {
		t.Fatalf("bad code: %d %v", code, body)
	}
	code, body = e.do(t, "POST", "/v1/jams/join", guest, map[string]any{"code": jcode, "displayName": "Ravi"})
	if code != 200 {
		t.Fatalf("join: %d %v", code, body)
	}
	guestPID := body["data"].(map[string]any)["you"].(string)

	// Non-members cannot open the socket.
	stranger := e.device(t)
	if _, res, err := e.dial(t, id, stranger); err == nil || res == nil || res.StatusCode != 403 {
		t.Fatalf("stranger dial: %v %v", err, res)
	}

	hc, _, err := e.dial(t, id, host)
	if err != nil {
		t.Fatal(err)
	}
	gc, _, err := e.dial(t, id, guest)
	if err != nil {
		t.Fatal(err)
	}
	if st := hc.next("state"); st["jam"].(map[string]any)["role"] != "admin" {
		t.Fatalf("host state: %v", st)
	}
	if st := gc.next("state"); st["jam"].(map[string]any)["role"] != "participant" {
		t.Fatalf("guest state: %v", st)
	}

	// Guest adds a track; host sees it.
	gc.send(map[string]any{"id": "c1", "type": "queue_add", "trackIds": []string{"saavn:s1"}})
	if ack := gc.next("ack"); ack["id"] != "c1" {
		t.Fatalf("ack: %v", ack)
	}
	for {
		st := hc.next("state")
		if len(st["jam"].(map[string]any)["queue"].(map[string]any)["entries"].([]any)) == 1 {
			break
		}
	}
	// Guest cannot control playback.
	gc.send(map[string]any{"id": "c2", "type": "play"})
	if er := gc.next("error"); er["code"] != "admin_only" || er["id"] != "c2" {
		t.Fatalf("guest play: %v", er)
	}
	gc.send(map[string]any{"type": "ping", "id": "p1", "clientTime": 1})
	if p := gc.next("pong"); p["clientTime"].(float64) != 1 {
		t.Fatalf("pong: %v", p)
	}
	hc.send(map[string]any{"id": "h1", "type": "play"})
	hc.next("ack")

	// Removing the guest closes their socket and blocks them.
	if code, body := e.do(t, "DELETE", "/v1/jams/"+id+"/participants/"+guestPID, host, nil); code != 200 {
		t.Fatalf("remove: %d %v", code, body)
	}
	gc.next("removed")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		if _, _, err := gc.c.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("close: %v", err)
			}
			break
		}
	}
	if code, body := e.do(t, "POST", "/v1/jams/"+id+"/commands", guest, map[string]any{"type": "queue_add", "trackIds": []string{"saavn:s2"}}); code != 403 || errCode(body) != "removed_from_jam" {
		t.Fatalf("removed guest command: %d %v", code, body)
	}
	if _, res, err := e.dial(t, id, guest); err == nil || res.StatusCode != 403 {
		t.Fatalf("removed guest reconnect: %v", err)
	}

	// Ending the Jam notifies the host's socket.
	if code, _ := e.do(t, "POST", "/v1/jams/"+id+"/end", host, nil); code != 200 {
		t.Fatalf("end: %d", code)
	}
	hc.next("ended")
	if code, body := e.do(t, "POST", "/v1/jams/join", stranger, map[string]any{"code": jcode}); code != 410 || errCode(body) != "jam_ended" {
		t.Fatalf("join ended: %d %v", code, body)
	}
}

func TestHealthAndMetrics(t *testing.T) {
	e := newEnv(t, setup{})
	if code, _ := e.do(t, "GET", "/readyz", "", nil); code != 200 {
		t.Fatalf("readyz: %d", code)
	}
	e.do(t, "GET", "/v1/search?q=kesariya", "", nil)
	res, err := http.Get(e.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(b), "swar_http_requests_total") {
		t.Fatalf("metrics: %s", b)
	}
}
