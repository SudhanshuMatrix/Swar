// Package auth issues and verifies device tokens.
//
// Swar is local-first and does not require accounts. A device registers
// once and receives a server-generated device ID plus an HMAC-signed bearer
// token. There are no passwords to store or leak. The token identifies the
// device to library, downloads and Jam endpoints; the server never trusts a
// client-supplied device ID or role.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("expired token")
)

type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokens(secret []byte, ttl time.Duration) *Tokens {
	return &Tokens{secret: secret, ttl: ttl, now: time.Now}
}

// NewDeviceID returns an unpredictable device identifier.
func NewDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	return "dev_" + hex.EncodeToString(b)
}

// Issue returns a token for deviceID and its expiry.
func (t *Tokens) Issue(deviceID string) (string, time.Time) {
	exp := t.now().Add(t.ttl).Truncate(time.Second)
	payload := deviceID + "|" + strconv.FormatInt(exp.Unix(), 10)
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return "v1." + p + "." + t.sign(p), exp
}

// Verify returns the device ID inside a valid, unexpired token.
func (t *Tokens) Verify(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" || len(token) > 512 {
		return "", ErrInvalidToken
	}
	if !hmac.Equal([]byte(t.sign(parts[1])), []byte(parts[2])) {
		return "", ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidToken
	}
	device, expStr, ok := strings.Cut(string(raw), "|")
	if !ok || !strings.HasPrefix(device, "dev_") {
		return "", ErrInvalidToken
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return "", ErrInvalidToken
	}
	if !t.now().Before(time.Unix(exp, 0)) {
		return "", ErrExpiredToken
	}
	return device, nil
}

func (t *Tokens) sign(payload string) string {
	m := hmac.New(sha256.New, t.secret)
	m.Write([]byte("swar-device-token:" + payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

type ctxKey struct{}

// WithDevice stores an authenticated device ID in ctx.
func WithDevice(ctx context.Context, deviceID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, deviceID)
}

// Device returns the authenticated device ID, or "".
func Device(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
