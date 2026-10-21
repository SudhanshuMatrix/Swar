package httpapi

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return w
}}

// compress gzips JSON answers for clients that accept it. Song lists are
// mostly repeated keys and shrink several times over, which makes every
// screen load faster on slow networks. Audio and WebSocket traffic pass
// through untouched.
func (s *Server) compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" || isRelayRequest(r) || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

// WriteHeader turns compression on for JSON bodies only; it is decided once,
// before the first byte goes out.
func (g *gzipWriter) WriteHeader(code int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		if code != http.StatusNoContent && code != http.StatusNotModified && code >= 200 &&
			h.Get("Content-Encoding") == "" && strings.HasPrefix(h.Get("Content-Type"), "application/json") {
			h.Del("Content-Length")
			h.Set("Content-Encoding", "gzip")
			g.gz = gzipPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	_ = http.NewResponseController(g.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}
