// Package plc tails the plc.directory export log to count network-wide identity
// operations and to derive the size of the federation (distinct PDS endpoints).
// It never emits a DID, handle, or PDS host as a label — only aggregate counts.
package plc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"anto.pt/x/atproto-exporter/internal/cursor"
	"anto.pt/x/atproto-exporter/internal/metrics"
)

const (
	defaultCount     = 1000
	maxPagesPerPoll  = 100 // bound catch-up work per poll cycle
	maxLineSize      = 1 << 20
	initialScanAlloc = 64 << 10
)

// Config configures a Poller.
type Config struct {
	BaseURL string
	Count   int
	Client  *http.Client
}

// entry is one line of the PLC export log. The DID is intentionally not decoded
// beyond being ignored; nothing identifying leaves this package.
type entry struct {
	Operation operation `json:"operation"`
	Nullified bool      `json:"nullified"`
	CreatedAt string    `json:"createdAt"`
}

type operation struct {
	Type     string    `json:"type"`
	Prev     *string   `json:"prev"`
	Services *services `json:"services"`
}

type services struct {
	AtprotoPDS *struct {
		Endpoint string `json:"endpoint"`
	} `json:"atproto_pds"`
}

// Poller fetches and processes the PLC export log incrementally.
type Poller struct {
	baseURL string
	count   int
	client  *http.Client
	m       *metrics.Metrics
	cur     *cursor.Store
	logger  *slog.Logger

	mu         sync.Mutex
	lastCursor string
	pdsSet     map[string]struct{}
}

// NewPoller builds a Poller. cur may be nil to disable cursor persistence.
func NewPoller(cfg Config, m *metrics.Metrics, cur *cursor.Store) *Poller {
	if cfg.Count <= 0 {
		cfg.Count = defaultCount
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 30 * time.Second}
	}
	p := &Poller{
		baseURL: cfg.BaseURL,
		count:   cfg.Count,
		client:  cfg.Client,
		m:       m,
		cur:     cur,
		logger:  slog.Default(),
		pdsSet:  map[string]struct{}{},
	}
	if cur != nil {
		if v, err := cur.Read(); err == nil {
			p.lastCursor = v
		}
	}
	return p
}

// Run polls on the given interval until ctx is cancelled. Poll errors are logged
// and counted but do not stop the loop (graceful degradation).
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	// Poll once immediately, then on the ticker.
	if err := p.PollOnce(ctx); err != nil && ctx.Err() == nil {
		p.logger.Warn("plc poll failed", "err", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.PollOnce(ctx); err != nil && ctx.Err() == nil {
				p.logger.Warn("plc poll failed", "err", err)
			}
		}
	}
}

// PollOnce fetches and processes new export entries, draining pagination up to a
// bounded number of pages, then updates the federation-size gauge.
func (p *Poller) PollOnce(ctx context.Context) error {
	start := time.Now()
	defer func() { p.m.ObservePLCPollDuration(time.Since(start).Seconds()) }()

	for pages := 0; pages < maxPagesPerPoll; pages++ {
		entries, err := p.fetchPage(ctx, p.lastCursor)
		if err != nil {
			p.m.IncPLCPollErrors()
			return err
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			p.process(e)
			if e.CreatedAt != "" {
				p.lastCursor = e.CreatedAt
			}
		}
		if p.cur != nil {
			_ = p.cur.Write(p.lastCursor)
		}
		if len(entries) < p.count {
			break
		}
	}

	p.mu.Lock()
	n := len(p.pdsSet)
	p.mu.Unlock()
	p.m.SetFederationPDS(n)
	return nil
}

func (p *Poller) process(e entry) {
	if e.Nullified {
		return // superseded fork operation; not part of the effective history
	}
	p.m.PLCOperation(opLabel(e.Operation))
	if e.Operation.Services != nil && e.Operation.Services.AtprotoPDS != nil {
		if ep := e.Operation.Services.AtprotoPDS.Endpoint; ep != "" {
			p.mu.Lock()
			p.pdsSet[ep] = struct{}{}
			p.mu.Unlock()
		}
	}
}

// opLabel maps a PLC operation to a bounded label: create | update | tombstone |
// other. A genesis plc_operation (no prev) or a legacy "create" is a creation;
// a plc_operation with a prev is an update; plc_tombstone is a tombstone.
func opLabel(op operation) string {
	switch op.Type {
	case "plc_tombstone":
		return "tombstone"
	case "plc_operation":
		if op.Prev == nil || *op.Prev == "" {
			return "create"
		}
		return "update"
	case "create":
		return "create"
	default:
		return "other"
	}
}

func (p *Poller) fetchPage(ctx context.Context, after string) ([]entry, error) {
	u := p.baseURL + "/export?count=" + strconv.Itoa(p.count)
	if after != "" {
		u += "&after=" + url.QueryEscape(after)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plc: unexpected status %d", resp.StatusCode)
	}

	var entries []entry
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, initialScanAlloc), maxLineSize)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			p.logger.Debug("plc: skipping malformed line", "err", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
