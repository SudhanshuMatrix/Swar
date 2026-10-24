package observability

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestHistogramExposition(t *testing.T) {
	m := NewMetrics()
	m.Observe("ytdlp_run", 30*time.Millisecond)
	m.Observe("ytdlp_run", 3*time.Second)
	m.Observe("ytdlp_run", 2*time.Minute)
	m.Inc("deepsearch_runs_total")
	var b bytes.Buffer
	m.WritePrometheus(&b)
	out := b.String()
	for _, want := range []string{
		"swar_deepsearch_runs_total 1\n",
		`swar_ytdlp_run_seconds_bucket{le="0.05"} 1` + "\n",
		`swar_ytdlp_run_seconds_bucket{le="5"} 2` + "\n",
		`swar_ytdlp_run_seconds_bucket{le="60"} 2` + "\n",
		`swar_ytdlp_run_seconds_bucket{le="+Inf"} 3` + "\n",
		"swar_ytdlp_run_seconds_count 3\n",
		"swar_ytdlp_run_seconds_sum 123.03\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if m.HistogramCount("ytdlp_run") != 3 {
		t.Fatal("count")
	}
	var nilM *Metrics
	nilM.Observe("x", time.Second) // must not panic
}
