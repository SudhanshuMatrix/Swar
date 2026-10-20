package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"swar/internal/catalog"
)

func TestRelayOnlyFetchesGooglevideo(t *testing.T) {
	for _, raw := range []string{"http://r1.googlevideo.com/x", "https://example.com/x", "https://googlevideo.com.evil.net/x", "file:///etc/passwd"} {
		if _, err := fetchRelay(context.Background(), raw, ""); err == nil {
			t.Errorf("%s should be refused", raw)
		}
	}
}

func TestRelayRouting(t *testing.T) {
	if p := relayPath("yt:abc"); p != "/v1/playback/yt:abc/audio" {
		t.Fatalf("relay path: %s", p)
	}
	if !isRelayRequest(httptest.NewRequest("GET", relayPath("yt:abc"), nil)) {
		t.Fatal("relay request should skip the request deadline")
	}
	if isRelayRequest(httptest.NewRequest("GET", "/v1/playback/yt:abc", nil)) {
		t.Fatal("plain playback is not a relay")
	}
	if !needsRelay(&catalog.Track{Provider: catalog.ProviderYouTube}) || needsRelay(&catalog.Track{Provider: catalog.ProviderSaavn}) {
		t.Fatal("only YouTube tracks are relayed")
	}
}
