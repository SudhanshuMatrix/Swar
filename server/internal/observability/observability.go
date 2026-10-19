// Package observability provides structured logging and a tiny metrics
// registry exposed in the Prometheus text format. It deliberately avoids a
// metrics client dependency: counters and gauges are all this server needs.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// NewLogger returns a JSON slog logger at the given level.
func NewLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

// Metrics is a registry of named counters and gauge callbacks.
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
	gauges   map[string]func() float64
}

func NewMetrics() *Metrics {
	return &Metrics{counters: map[string]*atomic.Int64{}, gauges: map[string]func() float64{}}
}

// Counter returns the counter with the given name, creating it if needed.
// Names may carry Prometheus labels, e.g. `http_requests_total{code="200"}`.
func (m *Metrics) Counter(name string) *atomic.Int64 {
	if m == nil {
		return new(atomic.Int64)
	}
	m.mu.RLock()
	c, ok := m.counters[name]
	m.mu.RUnlock()
	if ok {
		return c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok = m.counters[name]; !ok {
		c = new(atomic.Int64)
		m.counters[name] = c
	}
	return c
}

func (m *Metrics) Inc(name string) { m.Counter(name).Add(1) }

// Gauge registers a callback evaluated at scrape time.
func (m *Metrics) Gauge(name string, fn func() float64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.gauges[name] = fn
	m.mu.Unlock()
}

// WritePrometheus writes every metric in the text exposition format.
func (m *Metrics) WritePrometheus(w io.Writer) {
	m.mu.RLock()
	lines := make([]string, 0, len(m.counters)+len(m.gauges))
	for name, c := range m.counters {
		lines = append(lines, fmt.Sprintf("swar_%s %d", name, c.Load()))
	}
	gauges := make(map[string]func() float64, len(m.gauges))
	for k, v := range m.gauges {
		gauges[k] = v
	}
	m.mu.RUnlock()
	for name, fn := range gauges {
		lines = append(lines, fmt.Sprintf("swar_%s %g", name, fn()))
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}
