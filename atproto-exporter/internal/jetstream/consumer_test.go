package jetstream

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"anto.pt/x/atproto-exporter/internal/metrics"
	"anto.pt/x/atproto-exporter/internal/normalize"
	"anto.pt/x/atproto-exporter/internal/topn"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeConn struct {
	msgs      [][]byte
	i         int
	onExhaust error
}

func (f *fakeConn) Read(context.Context) ([]byte, error) {
	if f.i < len(f.msgs) {
		m := f.msgs[f.i]
		f.i++
		return m, nil
	}
	return nil, f.onExhaust
}
func (f *fakeConn) Close() error { return nil }

func newConsumerHarness(t *testing.T, cfg Config) (*Consumer, *metrics.Metrics, *Pipeline) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	allow := normalize.New([]string{"app.bsky.feed.like"}, []string{"en"})
	p := New(m, allow, topn.New(5, time.Hour), topn.New(5, time.Hour), nil, Options{Now: func() time.Time { return clockBase }})
	c, err := NewConsumer(cfg, p, m, nil)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	return c, m, p
}

func likeMsg(timeUS int) []byte {
	return []byte(`{"time_us":` + strconv.Itoa(timeUS) + `,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.like"}}`)
}

func TestConsumerProcessesThenReconnects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var dials int
	cfg := Config{
		Hosts: []string{"h1"},
		Dial: func(context.Context, string) (wsConn, error) {
			dials++
			if dials == 1 {
				return &fakeConn{msgs: [][]byte{likeMsg(1), likeMsg(2)}, onExhaust: io.EOF}, nil
			}
			cancel()
			return nil, context.Canceled
		},
		Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
	c, m, _ := newConsumerHarness(t, cfg)

	_ = c.Run(ctx)

	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.like", "create")); got != 2 {
		t.Errorf("processed likes = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.WSReconnectsCollector()); got < 1 {
		t.Errorf("ws reconnects = %v, want >= 1", got)
	}
	if got := testutil.ToFloat64(m.WSConnectedCollector()); got != 0 {
		t.Errorf("ws connected at end = %v, want 0", got)
	}
}

func TestConsumerRotatesHosts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var urls []string
	var dials int
	cfg := Config{
		Hosts: []string{"host-a", "host-b"},
		Dial: func(_ context.Context, url string) (wsConn, error) {
			urls = append(urls, url)
			dials++
			if dials == 1 {
				return &fakeConn{onExhaust: io.EOF}, nil // connects, immediately disconnects
			}
			cancel()
			return nil, context.Canceled
		},
		Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
	c, _, _ := newConsumerHarness(t, cfg)
	_ = c.Run(ctx)

	if len(urls) < 2 {
		t.Fatalf("dialed %d urls, want >= 2", len(urls))
	}
	if !strings.Contains(urls[0], "host-a") {
		t.Errorf("url[0] = %q, want host-a", urls[0])
	}
	if !strings.Contains(urls[1], "host-b") {
		t.Errorf("url[1] = %q, want host-b (rotation)", urls[1])
	}
}

func TestConsumerZstdDecode(t *testing.T) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderDict(zstdDict))
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	compressed := enc.EncodeAll(likeMsg(7), nil)
	enc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var dials int
	cfg := Config{
		Hosts: []string{"h1"},
		Zstd:  true,
		Dial: func(context.Context, string) (wsConn, error) {
			dials++
			if dials == 1 {
				return &fakeConn{msgs: [][]byte{compressed}, onExhaust: io.EOF}, nil
			}
			cancel()
			return nil, context.Canceled
		},
		Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
	c, m, _ := newConsumerHarness(t, cfg)
	_ = c.Run(ctx)

	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.like", "create")); got != 1 {
		t.Errorf("zstd-decoded like = %v, want 1", got)
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	b := &backoffState{base: 100 * time.Millisecond, max: time.Second, factor: 2, jitter: func() float64 { return 1 }}
	// With jitter=1 the value is the full (uncapped-then-capped) delay.
	got := []time.Duration{b.next(), b.next(), b.next(), b.next(), b.next()}
	// 100ms, 200ms, 400ms, 800ms, capped 1s
	wants := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second}
	for i, w := range wants {
		if got[i] != w {
			t.Errorf("next[%d] = %v, want %v", i, got[i], w)
		}
	}
	b.reset()
	if b.next() != 100*time.Millisecond {
		t.Errorf("after reset, next = %v, want 100ms", b.next())
	}
}

func TestBackoffJitterBounds(t *testing.T) {
	b := &backoffState{base: time.Second, max: time.Second, factor: 2, jitter: func() float64 { return 0 }}
	// jitter=0 -> half the delay (full-jitter lower bound).
	if got := b.next(); got != 500*time.Millisecond {
		t.Errorf("jitter=0 next = %v, want 500ms", got)
	}
}

func TestBuildURL(t *testing.T) {
	u := buildURL("jetstream1.us-east.bsky.network", []string{"app.bsky.feed.post", "app.bsky.feed.like"}, 12345, true)
	if !strings.HasPrefix(u, "wss://jetstream1.us-east.bsky.network/subscribe?") {
		t.Errorf("prefix wrong: %q", u)
	}
	for _, want := range []string{"wantedCollections=app.bsky.feed.post", "wantedCollections=app.bsky.feed.like", "cursor=12345", "compress=true"} {
		if !strings.Contains(u, want) {
			t.Errorf("url missing %q: %q", want, u)
		}
	}
	// No cursor when 0.
	u2 := buildURL("h", nil, 0, false)
	if strings.Contains(u2, "cursor=") {
		t.Errorf("url should omit cursor when 0: %q", u2)
	}
	if strings.Contains(u2, "compress=") {
		t.Errorf("url should omit compress when false: %q", u2)
	}
}
