package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"testing"
)

func TestJSONIsGzippedWhenAccepted(t *testing.T) {
	e := newEnv(t, setup{})
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/healthz", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	// A transport that leaves the body as sent, so the test sees the gzip.
	res, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", res.Header.Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(zr).Decode(&body); err != nil {
		t.Fatalf("body is not gzipped JSON: %v", err)
	}
}

func TestPlainWithoutAcceptEncoding(t *testing.T) {
	e := newEnv(t, setup{})
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/healthz", nil)
	res, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding = %q, want none", res.Header.Get("Content-Encoding"))
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("body is not plain JSON: %v", err)
	}
}
