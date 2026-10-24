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
	"time"
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

// Metrics is a registry of named counters, gauge callbacks and latency
// histograms.
type Metrics struct {
	mu         sync.RWMutex
	counters   map[string]*atomic.Int64
	gauges     map[string]func() float64
	histograms map[string]*histogram
}

func NewMetrics() *Metrics {
	return &Metrics{counters: map[string]*atomic.Int64{}, gauges: map[string]func() float64{}, histograms: map[string]*histogram{}}
}

// Latency bucket upper bounds, in seconds. They span a cache hit (well
// under 100ms) to a slow yt-dlp run (tens of seconds).
var latencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 60}

type histogram struct {
	buckets []atomic.Int64 // one per bound, plus +Inf
	count   atomic.Int64
	sumUS   atomic.Int64 // microseconds
}

// Observe records a duration in the histogram called name (without
// labels), exposed as name_seconds_bucket, _sum and _count.
func (m *Metrics) Observe(name string, d time.Duration) {
	if m == nil {
		return
	}
	m.mu.RLock()
	h, ok := m.histograms[name]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		if h, ok = m.histograms[name]; !ok {
			h = &histogram{buckets: make([]atomic.Int64, len(latencyBuckets)+1)}
			m.histograms[name] = h
		}
		m.mu.Unlock()
	}
	s := d.Seconds()
	i := sort.SearchFloat64s(latencyBuckets, s)
	h.buckets[i].Add(1)
	h.count.Add(1)
	h.sumUS.Add(d.Microseconds())
}

// Since records the time elapsed since start; handy with defer.
func (m *Metrics) Since(name string, start time.Time) { m.Observe(name, time.Since(start)) }

// HistogramCount returns how many observations name has (tests).
func (m *Metrics) HistogramCount(name string) int64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if h, ok := m.histograms[name]; ok {
		return h.count.Load()
	}
	return 0
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
	m.writeHistograms(w)
}

// writeHistograms writes cumulative buckets in the Prometheus format. They
// come after the sorted lines so each histogram's buckets stay in order.
func (m *Metrics) writeHistograms(w io.Writer) {
	m.mu.RLock()
	names := make([]string, 0, len(m.histograms))
	for name := range m.histograms {
		names = append(names, name)
	}
	m.mu.RUnlock()
	sort.Strings(names)
	for _, name := range names {
		m.mu.RLock()
		h := m.histograms[name]
		m.mu.RUnlock()
		base := "swar_" + name + "_seconds"
		var cum int64
		for i, b := range latencyBuckets {
			cum += h.buckets[i].Load()
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", base, b, cum)
		}
		cum += h.buckets[len(latencyBuckets)].Load()
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", base, cum)
		fmt.Fprintf(w, "%s_sum %g\n", base, float64(h.sumUS.Load())/1e6)
		fmt.Fprintf(w, "%s_count %d\n", base, h.count.Load())
	}
}
