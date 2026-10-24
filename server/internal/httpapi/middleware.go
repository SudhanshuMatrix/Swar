package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"swar/internal/auth"
)

type ctxKeyRequestID struct{}

func requestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRequestID{}).(string)
	return v
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController (and the WebSocket upgrade) reach
// the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// base wraps every request: request ID, panic recovery, security headers,
// structured access log and metrics.
func (s *Server) base(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		rid := hex.EncodeToString(b)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, rid)
		r = r.WithContext(ctx)

		h := w.Header()
		h.Set("X-Request-ID", rid)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")

		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("panic", "panic", p, "stack", string(debug.Stack()), "request_id", rid)
				if rec.status == 0 {
					fail(rec, http.StatusInternalServerError, "internal_error", "Something went wrong")
				}
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			s.metrics.Inc(`http_requests_total{code="` + strconv.Itoa(status/100) + `xx"}`)
			// Log the path only: query strings may carry tokens or queries.
			s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", status,
				"ms", time.Since(start).Milliseconds(), "bytes", rec.bytes, "request_id", rid)
		}()
		next.ServeHTTP(rec, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowAll := false
	allowed := map[string]bool{}
	for _, o := range s.cfg.CORSOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || allowed[origin]) {
			h := w.Header()
			if allowAll {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
			}
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Expose-Headers", "X-Swar-Maintenance, X-Swar-Popup")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limits applies the body size limit and a per-request deadline. WebSocket
// upgrades are exempt from the deadline (they are long-lived by design).
func (s *Server) limits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		}
		// WebSockets and relayed audio streams are long-lived by design.
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && !isRelayRequest(r) {
			ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
			defer cancel()
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimiter is a per-client token bucket.
type rateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	return &rateLimiter{rate: rps, burst: float64(burst), buckets: map[string]*bucket{}, now: time.Now}
}

func (l *rateLimiter) allow(key string, cost float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < cost {
		return false
	}
	b.tokens -= cost
	return true
}

func (l *rateLimiter) gc() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// The left-most entry is the original client when a trusted
			// proxy overwrites the header.
			ip, _, _ := strings.Cut(xff, ",")
			if ip = strings.TrimSpace(ip); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimit charges each request against the client IP's bucket. Deep
// search costs more because it runs an external process.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		cost := 1.0
		if r.URL.Path == "/v1/search/deep" {
			cost = 10
		}
		if !s.limiter.allow(s.clientIP(r), cost) {
			s.metrics.Inc("http_rate_limited_total")
			w.Header().Set("Retry-After", "2")
			fail(w, http.StatusTooManyRequests, "rate_limited", "Too many requests; slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate attaches the device ID from a valid bearer token, if any.
// Tokens may also arrive as ?access_token= for WebSocket clients that
// cannot set headers; access logs never include query strings.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		} else if strings.HasSuffix(r.URL.Path, "/ws") {
			token = r.URL.Query().Get("access_token")
		}
		if token != "" {
			device, err := s.tokens.Verify(token)
			if err != nil {
				s.writeErr(w, r, err)
				return
			}
			if !s.deviceKnown(r.Context(), device) {
				s.writeErr(w, r, auth.ErrInvalidToken)
				return
			}
			r = r.WithContext(auth.WithDevice(r.Context(), device))
		}
		next.ServeHTTP(w, r)
	})
}

// deviceKnown checks the device still exists (deleted devices lose access),
// caching positive answers briefly.
func (s *Server) deviceKnown(ctx context.Context, device string) bool {
	if _, ok := s.devCache.Get("dev:" + device); ok {
		return true
	}
	exists, err := s.store.DeviceExists(ctx, device)
	if err != nil || !exists {
		return false
	}
	s.devCache.Set("dev:"+device, true, 5*time.Minute)
	return true
}

// requireDevice wraps handlers that need an authenticated device.
func (s *Server) requireDevice(h func(w http.ResponseWriter, r *http.Request, device string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		device := auth.Device(r.Context())
		if device == "" {
			fail(w, http.StatusUnauthorized, "unauthorized", "This endpoint needs a device token; call POST /v1/auth/device first")
			return
		}
		h(w, r, device)
	}
}

func publicCache(w http.ResponseWriter, maxAge time.Duration) {
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(maxAge.Seconds())))
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }
