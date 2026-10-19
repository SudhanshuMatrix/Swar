package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenRoundTripAndTampering(t *testing.T) {
	tk := NewTokens([]byte(strings.Repeat("k", 32)), time.Hour)
	dev := NewDeviceID()
	tok, exp := tk.Issue(dev)
	if got, err := tk.Verify(tok); err != nil || got != dev {
		t.Fatalf("verify: %q %v", got, err)
	}
	if time.Until(exp) <= 0 {
		t.Fatal("expiry in the past")
	}
	parts := strings.Split(tok, ".")
	other, _ := NewTokens([]byte(strings.Repeat("k", 32)), time.Hour).Issue(NewDeviceID())
	for _, bad := range []string{
		"", "v1", tok + "x", "v2." + parts[1] + "." + parts[2],
		parts[0] + "." + strings.Split(other, ".")[1] + "." + parts[2], // payload swapped
	} {
		if _, err := tk.Verify(bad); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	if _, err := NewTokens([]byte(strings.Repeat("z", 32)), time.Hour).Verify(tok); !errors.Is(err, ErrInvalidToken) {
		t.Error("token valid under another secret")
	}
	tk.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := tk.Verify(tok); !errors.Is(err, ErrExpiredToken) {
		t.Errorf("expired token: %v", err)
	}
}
