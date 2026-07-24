package jetstream

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"anto.pt/x/atproto-exporter/internal/metrics"
	"anto.pt/x/atproto-exporter/internal/normalize"
	"anto.pt/x/atproto-exporter/internal/plc"
	"anto.pt/x/atproto-exporter/internal/topn"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func readFixture(t *testing.T) [][]byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "jetstream_sample.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		b := make([]byte, len(sc.Bytes()))
		copy(b, sc.Bytes())
		lines = append(lines, b)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestIntegrationReplayFixture(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	allow := normalize.New(
		[]string{"app.bsky.feed.post", "app.bsky.feed.like", "app.bsky.graph.follow"},
		[]string{"en", "ja"},
	)
	domains := topn.New(20, time.Hour)
	hashtags := topn.New(20, time.Hour)
	p := New(m, allow, domains, hashtags, nil, Options{Now: func() time.Time { return clockBase }})

	lines := readFixture(t)
	for _, ln := range lines {
		if err := p.Process(ln); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}

	check := func(kind, coll, op string, want float64) {
		t.Helper()
		got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues(kind, coll, op))
		if got != want {
			t.Errorf("events{%s,%s,%s} = %v, want %v", kind, coll, op, got, want)
		}
	}
	check("commit", "app.bsky.feed.post", "create", 3)
	check("commit", "app.bsky.feed.post", "delete", 1)
	check("commit", "app.bsky.feed.like", "create", 1)
	check("commit", "app.bsky.graph.follow", "create", 1)
	check("commit", "other", "create", 1)
	check("identity", "", "update", 1)
	check("account", "", "create", 1)
	check("account", "", "delete", 1)

	if got := testutil.ToFloat64(m.PostsTotalVec().WithLabelValues("en")); got != 2 {
		t.Errorf("posts{en} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.PostsTotalVec().WithLabelValues("ja")); got != 1 {
		t.Errorf("posts{ja} = %v, want 1", got)
	}

	// Top domains/hashtags from structured facets/embeds only.
	dtop := map[string]int64{}
	for _, e := range domains.Top(clockBase) {
		dtop[e.Key] = e.Count
	}
	if dtop["example.com"] != 2 {
		t.Errorf("domain example.com = %d, want 2", dtop["example.com"])
	}
	if dtop["blog.example.org"] != 1 {
		t.Errorf("domain blog.example.org = %d, want 1", dtop["blog.example.org"])
	}
	htop := hashtags.Top(clockBase)
	if len(htop) != 1 || htop[0].Key != "golang" || htop[0].Count != 2 {
		t.Errorf("hashtags = %v, want [golang:2]", htop)
	}
}

// TestIntegrationRewindIdempotent proves that replaying an overlapping segment
// (as happens after a cursor rewind on reconnect) does not double-count.
func TestIntegrationRewindIdempotent(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	allow := normalize.New([]string{"app.bsky.feed.post", "app.bsky.feed.like", "app.bsky.graph.follow"}, []string{"en", "ja"})
	p := New(m, allow, topn.New(20, time.Hour), topn.New(20, time.Hour), nil, Options{Now: func() time.Time { return clockBase }})

	lines := readFixture(t)
	for _, ln := range lines {
		_ = p.Process(ln)
	}
	before := testutil.ToFloat64(m.EventsProcessedCollector())
	postCreateBefore := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.post", "create"))
	lastBefore := p.LastTimeUS()

	// Simulate reconnect rewind: replay the last 4 lines (all time_us <= last).
	for _, ln := range lines[len(lines)-4:] {
		_ = p.Process(ln)
	}

	if after := testutil.ToFloat64(m.EventsProcessedCollector()); after != before {
		t.Errorf("events_processed after rewind replay = %v, want unchanged %v", after, before)
	}
	if after := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.post", "create")); after != postCreateBefore {
		t.Errorf("post creates after rewind replay = %v, want unchanged %v", after, postCreateBefore)
	}
	if p.LastTimeUS() != lastBefore {
		t.Errorf("LastTimeUS moved on rewind replay: %d -> %d", lastBefore, p.LastTimeUS())
	}
}

// TestGracefulDegradation proves the PLC source failing does not stop event
// processing: while the poller hits a failing server, pipeline events keep
// flowing and the poll-error counter climbs.
func TestGracefulDegradation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	allow := normalize.New([]string{"app.bsky.feed.like"}, []string{"en"})
	p := New(m, allow, topn.New(5, time.Hour), topn.New(5, time.Hour), nil, Options{Now: func() time.Time { return clockBase }})
	poller := plc.NewPoller(plc.Config{BaseURL: srv.URL, Count: 10}, m, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go poller.Run(ctx, 5*time.Millisecond)

	// Events keep flowing while PLC is broken.
	for i := 1; i <= 5; i++ {
		if err := p.Process(likeMsg(i)); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.like", "create")); got != 5 {
		t.Errorf("likes = %v, want 5 (events flow despite PLC failure)", got)
	}

	// The PLC poller records errors but does not crash.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testutil.ToFloat64(m.PLCPollErrorsCollector()) >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("expected at least one PLC poll error while degraded")
}
