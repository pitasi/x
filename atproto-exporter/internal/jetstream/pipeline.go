// Package jetstream consumes the Bluesky Jetstream firehose and turns each event
// into bounded-cardinality metrics. This file holds the pure event pipeline
// (bytes in, metrics out); the network transport lives in consumer.go.
package jetstream

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"anto.pt/x/atproto-exporter/internal/cursor"
	"anto.pt/x/atproto-exporter/internal/metrics"
	"anto.pt/x/atproto-exporter/internal/normalize"
	"anto.pt/x/atproto-exporter/internal/topn"
)

// event is the top-level Jetstream message. Note: we deliberately do not decode
// the DID or handle beyond what we ignore — no PII ever reaches a metric or log.
type event struct {
	TimeUS   int64     `json:"time_us"`
	Kind     string    `json:"kind"`
	Commit   *commit   `json:"commit"`
	Account  *account  `json:"account"`
	Identity *struct{} `json:"identity"`
}

type commit struct {
	Operation  string          `json:"operation"`
	Collection string          `json:"collection"`
	Record     json.RawMessage `json:"record"`
}

type account struct {
	Active bool   `json:"active"`
	Status string `json:"status"`
}

type postRecord struct {
	Langs  []string `json:"langs"`
	Facets []facet  `json:"facets"`
	Embed  *embed   `json:"embed"`
}

type facet struct {
	Features []feature `json:"features"`
}

type feature struct {
	Type string `json:"$type"`
	URI  string `json:"uri"`
	Tag  string `json:"tag"`
}

type embed struct {
	Type     string `json:"$type"`
	External *struct {
		URI string `json:"uri"`
	} `json:"external"`
}

// Options configures a Pipeline.
type Options struct {
	// Now returns the current time (injectable for tests). Defaults to time.Now.
	Now func() time.Time
	// PersistEvery writes the cursor every N processed events. 0 disables
	// count-based persistence (Flush still writes on demand). Ignored if the
	// cursor store is nil.
	PersistEvery int
}

// Pipeline processes decoded Jetstream messages into metrics. It is safe for use
// from a single consumer goroutine; Process is not intended for concurrent calls.
type Pipeline struct {
	m        *metrics.Metrics
	allow    *normalize.Allowlist
	domains  *topn.Window
	hashtags *topn.Window
	cur      *cursor.Store
	now      func() time.Time

	persistEvery int

	mu           sync.Mutex
	lastTimeUS   int64
	sincePersist int
}

// New builds a Pipeline. cur may be nil to disable cursor persistence.
func New(m *metrics.Metrics, allow *normalize.Allowlist, domains, hashtags *topn.Window, cur *cursor.Store, opts Options) *Pipeline {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Pipeline{
		m:            m,
		allow:        allow,
		domains:      domains,
		hashtags:     hashtags,
		cur:          cur,
		now:          now,
		persistEvery: opts.PersistEvery,
	}
}

// LastTimeUS returns the highest time_us processed so far.
func (p *Pipeline) LastTimeUS() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTimeUS
}

// Process decodes and handles a single Jetstream message. Messages with a
// time_us at or below the last processed value are skipped, making cursor rewind
// on reconnect idempotent. A JSON decode error is returned; the caller decides
// whether to continue.
func (p *Pipeline) Process(raw []byte) error {
	var e event
	if err := json.Unmarshal(raw, &e); err != nil {
		return err
	}

	p.mu.Lock()
	if e.TimeUS <= p.lastTimeUS {
		p.mu.Unlock()
		return nil // duplicate / already-processed (rewind overlap)
	}
	p.lastTimeUS = e.TimeUS
	p.sincePersist++
	persist := p.cur != nil && p.persistEvery > 0 && p.sincePersist >= p.persistEvery
	if persist {
		p.sincePersist = 0
	}
	last := p.lastTimeUS
	p.mu.Unlock()

	p.updateLag(e.TimeUS)

	switch e.Kind {
	case "commit":
		p.handleCommit(e)
	case "identity":
		p.m.ObserveEvent("identity", "", "update")
	case "account":
		p.handleAccount(e)
	}

	if persist {
		_ = p.cur.WriteInt64(last)
	}
	return nil
}

func (p *Pipeline) updateLag(timeUS int64) {
	if timeUS <= 0 {
		return
	}
	evt := time.UnixMicro(timeUS)
	p.m.SetFirehoseLag(p.now().Sub(evt).Seconds())
}

func (p *Pipeline) handleCommit(e event) {
	if e.Commit == nil {
		p.m.ObserveEvent("commit", "other", "")
		return
	}
	coll := p.allow.Collection(e.Commit.Collection)
	p.m.ObserveEvent("commit", coll, e.Commit.Operation)

	// Post-specific derivation only for creates/updates that carry a record.
	if e.Commit.Collection == "app.bsky.feed.post" &&
		(e.Commit.Operation == "create" || e.Commit.Operation == "update") &&
		len(e.Commit.Record) > 0 {
		p.handlePostRecord(e.Commit.Record, e.TimeUS)
	}
}

func (p *Pipeline) handlePostRecord(raw json.RawMessage, timeUS int64) {
	var rec postRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return // malformed record: skip enrichment, event already counted
	}

	lang := "unknown"
	if len(rec.Langs) > 0 {
		lang = p.allow.Language(rec.Langs[0])
	} else {
		lang = p.allow.Language("")
	}
	p.m.ObservePost(lang)

	evt := time.UnixMicro(timeUS)
	for _, f := range rec.Facets {
		for _, feat := range f.Features {
			switch feat.Type {
			case "app.bsky.richtext.facet#link":
				if d := domainOf(feat.URI); d != "" {
					p.domains.Add(d, evt)
				}
			case "app.bsky.richtext.facet#tag":
				if tag := strings.TrimSpace(feat.Tag); tag != "" {
					p.hashtags.Add(strings.ToLower(tag), evt)
				}
			}
		}
	}
	if rec.Embed != nil && rec.Embed.External != nil {
		if d := domainOf(rec.Embed.External.URI); d != "" {
			p.domains.Add(d, evt)
		}
	}
}

func (p *Pipeline) handleAccount(e event) {
	// Account events signal status changes. Map activation to "create" and
	// deactivation/tombstone to "delete" — an approximation, but bounded and
	// meaningful for account-lifecycle rate panels.
	op := "create"
	if e.Account != nil && !e.Account.Active {
		op = "delete"
	}
	p.m.ObserveEvent("account", "", op)
}

// domainOf extracts the registrable-ish host from a URL, stripping a leading
// "www." and lowercasing. Returns "" for anything unparseable.
func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}
