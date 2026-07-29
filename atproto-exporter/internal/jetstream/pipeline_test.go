package jetstream

import (
	"testing"
	"time"

	"anto.pt/x/atproto-exporter/internal/metrics"
	"anto.pt/x/atproto-exporter/internal/normalize"
	"anto.pt/x/atproto-exporter/internal/topn"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

var clockBase = time.Unix(1_700_000_001, 0) // ~1s after the fixture events

func newTestPipeline(t *testing.T) (*Pipeline, *metrics.Metrics, *topn.Window, *topn.Window) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	allow := normalize.New(
		[]string{"app.bsky.feed.post", "app.bsky.feed.like", "app.bsky.graph.follow"},
		[]string{"en", "ja"},
	)
	domains := topn.New(20, time.Hour)
	hashtags := topn.New(20, time.Hour)
	p := New(m, allow, domains, hashtags, nil, Options{Now: func() time.Time { return clockBase }})
	return p, m, domains, hashtags
}

func TestPipelineCommitCounts(t *testing.T) {
	p, m, _, _ := newTestPipeline(t)
	msgs := []string{
		`{"time_us":1,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.post","record":{"langs":["en"]}}}`,
		`{"time_us":2,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.like"}}`,
		`{"time_us":3,"kind":"commit","commit":{"operation":"create","collection":"com.example.weird"}}`,
		`{"time_us":4,"kind":"commit","commit":{"operation":"delete","collection":"app.bsky.feed.post"}}`,
	}
	for _, s := range msgs {
		if err := p.Process([]byte(s)); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.post", "create")); got != 1 {
		t.Errorf("post create = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "other", "create")); got != 1 {
		t.Errorf("weird->other create = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.post", "delete")); got != 1 {
		t.Errorf("post delete = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.PostsTotalVec().WithLabelValues("en")); got != 1 {
		t.Errorf("posts en = %v, want 1", got)
	}
}

func TestPipelineIdempotentSkip(t *testing.T) {
	p, m, _, _ := newTestPipeline(t)
	msg := `{"time_us":100,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.like"}}`
	dup := `{"time_us":100,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.like"}}`
	older := `{"time_us":50,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.like"}}`

	_ = p.Process([]byte(msg))
	_ = p.Process([]byte(dup))   // same time_us -> skip
	_ = p.Process([]byte(older)) // older time_us -> skip

	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("commit", "app.bsky.feed.like", "create")); got != 1 {
		t.Errorf("like create = %v, want 1 (dups skipped)", got)
	}
	if p.LastTimeUS() != 100 {
		t.Errorf("LastTimeUS = %d, want 100", p.LastTimeUS())
	}
}

func TestPipelineIdentityAndAccount(t *testing.T) {
	p, m, _, _ := newTestPipeline(t)
	_ = p.Process([]byte(`{"time_us":1,"kind":"identity","identity":{"handle":"x"}}`))
	_ = p.Process([]byte(`{"time_us":2,"kind":"account","account":{"active":true}}`))
	_ = p.Process([]byte(`{"time_us":3,"kind":"account","account":{"active":false,"status":"deleted"}}`))

	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("identity", "", "update")); got != 1 {
		t.Errorf("identity = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("account", "", "create")); got != 1 {
		t.Errorf("account active = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.EventsTotalVec().WithLabelValues("account", "", "delete")); got != 1 {
		t.Errorf("account inactive = %v, want 1", got)
	}
}

func TestPipelineDomainAndHashtagExtraction(t *testing.T) {
	p, _, domains, hashtags := newTestPipeline(t)
	// time_us must be within the rolling window of clockBase (~1s before it).
	msg := `{"time_us":1700000000000000,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.post","record":{
		"langs":["en"],
		"facets":[{"features":[
			{"$type":"app.bsky.richtext.facet#link","uri":"https://www.example.com/a"},
			{"$type":"app.bsky.richtext.facet#tag","tag":"golang"}
		]}],
		"embed":{"$type":"app.bsky.embed.external","external":{"uri":"https://blog.example.org/p"}}
	}}}`
	if err := p.Process([]byte(msg)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	dtop := domains.Top(clockBase)
	dkeys := map[string]bool{}
	for _, e := range dtop {
		dkeys[e.Key] = true
	}
	if !dkeys["example.com"] { // www. stripped
		t.Errorf("domains missing example.com: %v", dtop)
	}
	if !dkeys["blog.example.org"] {
		t.Errorf("domains missing blog.example.org: %v", dtop)
	}
	htop := hashtags.Top(clockBase)
	if len(htop) != 1 || htop[0].Key != "golang" {
		t.Errorf("hashtags = %v, want [golang]", htop)
	}
}

func TestPipelineFirehoseLag(t *testing.T) {
	p, m, _, _ := newTestPipeline(t)
	// event at t=1_700_000_000s, clock at +1s => lag ~1s
	msg := `{"time_us":1700000000000000,"kind":"commit","commit":{"operation":"create","collection":"app.bsky.feed.like"}}`
	_ = p.Process([]byte(msg))
	lag := testutil.ToFloat64(m.FirehoseLagCollector())
	if lag < 0.9 || lag > 1.1 {
		t.Errorf("firehose lag = %v, want ~1s", lag)
	}
}

func TestPipelineMalformedJSON(t *testing.T) {
	p, _, _, _ := newTestPipeline(t)
	if err := p.Process([]byte(`{not json`)); err == nil {
		t.Errorf("Process(malformed) = nil, want error")
	}
}
