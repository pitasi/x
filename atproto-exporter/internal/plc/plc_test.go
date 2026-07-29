package plc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"anto.pt/x/atproto-exporter/internal/cursor"
	"anto.pt/x/atproto-exporter/internal/metrics"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fixtureServer serves the sample on the first (cursor-less) request and an
// empty body once a cursor is supplied, simulating pagination drain.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "plc_sample.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "" {
			return // drained
		}
		_, _ = w.Write(body)
	}))
}

func newPoller(t *testing.T, base string, cur *cursor.Store) (*Poller, *metrics.Metrics) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	p := NewPoller(Config{BaseURL: base, Count: 1000}, m, cur)
	return p, m
}

func TestPollOnceCountsOperations(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	p, m := newPoller(t, srv.URL, nil)

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	// create: genesis plc_operation (aaa) + legacy create (ddd) + genesis (ccc) = 3
	if got := testutil.ToFloat64(m.PLCOperationsVec().WithLabelValues("create")); got != 3 {
		t.Errorf("create = %v, want 3", got)
	}
	if got := testutil.ToFloat64(m.PLCOperationsVec().WithLabelValues("update")); got != 1 {
		t.Errorf("update = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.PLCOperationsVec().WithLabelValues("tombstone")); got != 1 {
		t.Errorf("tombstone = %v, want 1", got)
	}
}

func TestPollOnceDistinctPDS(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	p, m := newPoller(t, srv.URL, nil)

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	// pds1 + pds2 = 2 distinct; the nullified pds2 op is ignored but doesn't
	// change the count. Legacy "create" op has no atproto_pds service.
	if got := testutil.ToFloat64(m.FederationPDSCollector()); got != 2 {
		t.Errorf("federation pds = %v, want 2", got)
	}
}

func TestPollOnceNullifiedSkipped(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	p, m := newPoller(t, srv.URL, nil)
	_ = p.PollOnce(context.Background())

	// The nullified entry (eee) is a genesis plc_operation; if counted it would
	// bump create to 4. It must be skipped.
	if got := testutil.ToFloat64(m.PLCOperationsVec().WithLabelValues("create")); got != 3 {
		t.Errorf("create with nullified = %v, want 3 (nullified skipped)", got)
	}
}

func TestPollOncePersistsCursor(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cur := cursor.NewStore(filepath.Join(t.TempDir(), "plc.cursor"))
	p, _ := newPoller(t, srv.URL, cur)

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	got, _ := cur.Read()
	if got != "2024-01-01T00:00:06.000Z" {
		t.Errorf("cursor = %q, want last createdAt", got)
	}
}

func TestPollOnceResumesFromCursor(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cur := cursor.NewStore(filepath.Join(t.TempDir(), "plc.cursor"))
	_ = cur.Write("2024-01-01T00:00:06.000Z") // already caught up
	p, m := newPoller(t, srv.URL, cur)

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	// Server returns empty when after= is set, so nothing new is counted.
	if got := testutil.ToFloat64(m.PLCOperationsVec().WithLabelValues("create")); got != 0 {
		t.Errorf("create after resume = %v, want 0", got)
	}
}

func TestPollOnceServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	p, m := newPoller(t, srv.URL, nil)

	if err := p.PollOnce(context.Background()); err == nil {
		t.Error("PollOnce on 500 = nil, want error")
	}
	if got := testutil.ToFloat64(m.PLCPollErrorsCollector()); got < 1 {
		t.Errorf("poll errors = %v, want >= 1", got)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	p, _ := newPoller(t, srv.URL, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}
