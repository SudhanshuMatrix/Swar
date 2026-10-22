package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := LoadFrom(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8765" || !c.MusicProviderDeezerEnabled || !c.MusicProviderITunesEnabled || c.DeepSearchPlayback || !c.DeepSearchEnabled || len(c.TokenSecret) == 0 {
		t.Fatalf("unexpected defaults: %+v", c)
	}

	// Test PORT environment fallback for Render
	cPort, err := LoadFrom(env(map[string]string{"PORT": "10000"}))
	if err != nil {
		t.Fatal(err)
	}
	if cPort.Addr != ":10000" {
		t.Fatalf("expected :10000 from PORT, got %s", cPort.Addr)
	}
}

func TestProductionRequiresSecret(t *testing.T) {
	if _, err := LoadFrom(env(map[string]string{"SWAR_ENV": "production"})); err == nil {
		t.Fatal("production without secret must fail")
	}
	_, err := LoadFrom(env(map[string]string{"SWAR_ENV": "production", "SWAR_TOKEN_SECRET": "short"}))
	if err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("short secret must fail: %v", err)
	}
	if _, err := LoadFrom(env(map[string]string{"SWAR_ENV": "production", "SWAR_TOKEN_SECRET": strings.Repeat("s", 40)})); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidValues(t *testing.T) {
	cases := map[string]string{
		"SWAR_RATE_LIMIT_RPS":    "abc",
		"DEEPSEARCH_MAX_RESULTS": "500",
		"MUSIC_SEARCH_TIMEOUT":   "5",
		"SWAR_ENV":               "staging",
		"JAM_MAX_PARTICIPANTS":   "1",
	}
	for k, v := range cases {
		if _, err := LoadFrom(env(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%s should be rejected", k, v)
		}
	}
}
