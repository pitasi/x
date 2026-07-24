package metrics

import (
	"strings"
	"testing"
	"time"

	"anto.pt/x/atproto-exporter/internal/topn"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

var base = time.Unix(1_700_000_000, 0)

func TestAllMetricsRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)
	// Touch label-bearing metrics so they appear in the gather output.
	m.ObserveEvent("commit", "app.bsky.feed.post", "create")
	m.ObservePost("en")
	m.PLCOperation("plc_operation")
	m.SetFirehoseLag(1.5)
	m.SetFederationPDS(3)
	m.SetWSConnected(true)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, mf := range mfs {
		got[mf.GetName()] = true
	}
	want := []string{
		"atproto_events_total",
		"atproto_posts_total",
		"atproto_firehose_lag_seconds",
		"atproto_events_processed_total",
		"atproto_plc_operations_total",
		"atproto_federation_pds_count",
		"atproto_exporter_ws_connected",
		"atproto_exporter_ws_reconnects_total",
		"atproto_exporter_plc_poll_errors_total",
		"atproto_exporter_plc_poll_duration_seconds",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("metric %q not registered", name)
		}
	}
	_ = m
}

func TestObserveEventValue(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)
	m.ObserveEvent("commit", "app.bsky.feed.post", "create")
	m.ObserveEvent("commit", "app.bsky.feed.post", "create")

	const want = `
# HELP atproto_events_total Total AT Protocol firehose events by kind, collection and operation.
# TYPE atproto_events_total counter
atproto_events_total{collection="app.bsky.feed.post",kind="commit",operation="create"} 2
`
	if err := testutil.CollectAndCompare(m.eventsTotal, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

func entries(pairs ...any) []topn.Entry {
	var out []topn.Entry
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, topn.Entry{Key: pairs[i].(string), Count: int64(pairs[i+1].(int))})
	}
	return out
}

func TestTopNGaugeSyncAndEvict(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)

	m.TopDomains.Sync(topn.Snapshot{Top: entries("a.com", 5, "b.com", 3)})
	if n := testutil.CollectAndCount(m.topDomains); n != 2 {
		t.Fatalf("after first sync: %d series, want 2", n)
	}
	if v := testutil.ToFloat64(m.topDomains.WithLabelValues("a.com")); v != 5 {
		t.Errorf("a.com = %v, want 5", v)
	}

	// b.com evicted, c.com added; Removed must delete b.com's series.
	m.TopDomains.Sync(topn.Snapshot{
		Top:     entries("a.com", 9, "c.com", 4),
		Removed: []string{"b.com"},
	})
	if n := testutil.CollectAndCount(m.topDomains); n != 2 {
		t.Fatalf("after evict sync: %d series, want 2 (b.com deleted)", n)
	}
	if v := testutil.ToFloat64(m.topDomains.WithLabelValues("a.com")); v != 9 {
		t.Errorf("a.com updated = %v, want 9", v)
	}
}

func TestTopNGaugeCardinalityBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)
	// Drive a real topn.Window so Snapshot bookkeeping is exercised end-to-end.
	w := topn.New(3, time.Hour)
	for _, k := range []string{"a", "a", "a", "b", "b", "c", "d", "e"} {
		w.Add(k, base)
	}
	for range 10 {
		w.Add("f", base) // f becomes hot; older keys should evict
		m.TopHashtags.Sync(w.Snapshot(base))
		if n := testutil.CollectAndCount(m.topHashtags); n > 3 {
			t.Fatalf("top hashtags cardinality = %d, exceeds N=3", n)
		}
	}
}
