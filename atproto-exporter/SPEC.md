# Spec: atproto-exporter

A single, lightweight, self-hosted Prometheus exporter that turns public AT Protocol
(Bluesky "atmosphere") data into bounded-cardinality metrics for a public Grafana
dashboard. No relay, no AppView, no PDS, no database — in-memory state plus small
on-disk cursors only.

## Assumptions

These are the choices I'm proceeding with. Correct any before implementation begins.

1. **Module path** is `anto.pt/x/atproto-exporter` (matches sibling projects in the `x` monorepo). Go **1.26**.
2. **docker-compose stands up the full local stack** — exporter + Prometheus + Grafana with the dashboard auto-provisioned — so anyone can `docker compose up` and see it work. The same exporter is independently scrapeable by your real Prometheus/Mimir.
3. **Top hashtags ship in v1** alongside top domains, reusing one generic bounded top-N engine.
4. **Jetstream zstd is supported** (negotiated via the connection option, decoded with the published Jetstream dictionary), with plain JSON as the fallback wire format.
5. **WebSocket client:** `github.com/coder/websocket` (context-aware, idiomatic). **zstd:** `github.com/klauspost/compress/zstd`. **Metrics:** `github.com/prometheus/client_golang`. **Logging:** stdlib `log/slog` (JSON handler). **Config:** stdlib `flag` with env-var fallbacks — no config framework.
6. **License/visibility:** part of the existing `x` repo; no new repository is created.

## Objective

**What:** Derive live, network-wide analytics for the AT Protocol atmosphere from two
public, unauthenticated sources and expose them as Prometheus metrics.

**Why:** Power a public Grafana dashboard showing the health and shape of the network
(events/sec, posts by language, account growth, federation size, top domains/hashtags,
firehose lag) without operating any heavy indexing infrastructure or building a
surveillance surface.

**Users:**
- **Operator (you):** runs one small Go binary next to Prometheus/Mimir + Grafana.
- **Dashboard viewers (public):** read aggregate network stats. They must never be able to see or rank any individual account.

**Success looks like:** the binary runs with zero flags, holds a live Jetstream
connection, tails the PLC export log, and serves `/metrics` + `/healthz`; every metric
obeys the bounded-cardinality contract; the provisioned Grafana dashboard renders all
panels from a fresh `docker compose up`.

**Data sources (public, no auth):**
- **Jetstream** — Bluesky WebSocket firehose as filterable JSON. `wss://<host>/subscribe`. Time-based cursors (unix microseconds). Primary real-time source. Hosts: `jetstream1.us-east.bsky.network`, `jetstream2.us-east.bsky.network`, `jetstream1.us-west.bsky.network`, `jetstream2.us-west.bsky.network`.
- **plc.directory export log** — `GET {base}/export?after=<cursor>&count=<n>`, newline-delimited JSON operations. Tailed with a persisted `after=` cursor to count network-wide identity operations and derive federation size (distinct PDS endpoints).

## Tech Stack

- **Language:** Go 1.26, idiomatic, `context`-driven shutdown.
- **Module:** `anto.pt/x/atproto-exporter`
- **Dependencies:**
  - `github.com/coder/websocket` — Jetstream WS client
  - `github.com/klauspost/compress/zstd` — Jetstream zstd decompression
  - `github.com/prometheus/client_golang` — metrics + registry + HTTP handler
  - stdlib: `log/slog`, `flag`, `net/http`, `container/heap`, `encoding/json`
- **Runtime:** distroless/scratch container, single static binary.

## Commands

```
Build:        go build ./cmd/atproto-exporter
Run (dev):    go run ./cmd/atproto-exporter            # zero flags, sane defaults
Test:         go test ./...
Test (race):  go test -race -cover ./...
Lint/vet:     go vet ./... && gofmt -l .
Tidy:         go mod tidy
Container:    docker build -t atproto-exporter .
Local stack:  docker compose -f deploy/docker-compose.yml up --build
              # exporter :9200, Prometheus :9090, Grafana :3000 (dashboard auto-provisioned)
Update dict:  make update-zstd-dict   # refresh the embedded Jetstream zstd dictionary
```

## Project Structure

```
atproto-exporter/
  cmd/atproto-exporter/main.go     → wiring: config, logger, HTTP server, goroutines, signals
  internal/config/                 → flag+env parsing, defaults, allowlists
  internal/jetstream/              → WS consumer: connect, zstd, reconnect (backoff+jitter), failover
  internal/plc/                    → PLC export poll loop, op parsing, PDS-set tracking
  internal/normalize/              → collection allowlist bucketing, language normalization (pure)
  internal/topn/                   → generic bounded min-heap top-N + stale-series eviction (pure)
  internal/cursor/                 → cursor read/write/round-trip (atomic file writes)
  internal/metrics/                → prometheus collectors + registry, top-N gauge sync
  testdata/                        → jetstream sample fixture(s), plc export sample
  deploy/
    docker-compose.yml             → exporter + prometheus + grafana
    prometheus/prometheus.yml      → scrape config for the exporter
    grafana/provisioning/          → datasource + dashboard provisioning
    grafana/dashboards/atproto.json→ importable dashboard (Prometheus datasource variable)
  docs/adr/
    0001-cardinality-strategy.md
    0002-cursor-and-replay-model.md
  Dockerfile
  README.md
  SPEC.md
  go.mod
```

Tests live next to the code they exercise (`foo.go` → `foo_test.go`), Go convention.
The Jetstream replay integration test lives in `internal/jetstream/` and reads `testdata/`.

## Code Style

Idiomatic Go: `gofmt`, small packages with clear boundaries, pure logic separated from
I/O so it's trivially testable. Errors wrapped with `%w`; `context.Context` first arg on
anything blocking. Structured logging via `slog` — **never** log a handle, DID, IP, or
record key.

```go
// normalize buckets a raw NSID to an allowlisted collection label or "other".
// The allowlist is a set so lookup is O(1); this is the single hottest path in
// the event pipeline, called once per commit.
func (a Allowlist) Collection(nsid string) string {
	if _, ok := a.set[nsid]; ok {
		return nsid
	}
	return "other"
}

// Language normalizes a dirty, user-supplied BCP-47 tag to an allowlisted ISO-639
// code or "other": lowercased, region/script subtags dropped ("en-US" -> "en").
func (a Allowlist) Language(tag string) string {
	if tag == "" {
		return "unknown"
	}
	base, _, _ := strings.Cut(strings.ToLower(tag), "-")
	if _, ok := a.langs[base]; ok {
		return base
	}
	return "other"
}
```

Bounded top-N eviction is explicit — series leaving the top set are `Delete`d from the
gauge vector so label churn never accumulates:

```go
// Sync replaces the exported series with exactly the current top-N. Any label
// value present last cycle but absent now is deleted, keeping cardinality <= N.
func (g *TopNGauge) Sync(top []Entry) {
	next := make(map[string]struct{}, len(top))
	for _, e := range top {
		g.vec.WithLabelValues(e.Key).Set(float64(e.Count))
		next[e.Key] = struct{}{}
	}
	for k := range g.current {
		if _, keep := next[k]; !keep {
			g.vec.DeleteLabelValues(k)
		}
	}
	g.current = next
}
```

## Metrics Contract (HARD CONSTRAINT: bounded cardinality)

No label may take unbounded user-controlled values. **Never** label by DID, handle, IP,
or record key. Every user-supplied dimension is allowlisted with an `other` bucket.

**Event-derived (Jetstream):**
- `atproto_events_total{kind,collection,operation}` — counter. `kind ∈ commit|identity|account`; `operation ∈ create|update|delete`; `collection` restricted to the configurable NSID allowlist, else `other`.
- `atproto_posts_total{lang}` — counter. `lang` restricted to the ~40-code ISO allowlist, else `other` (or `unknown` when empty).
- `atproto_firehose_lag_seconds` — gauge. `now - time_us` of the last processed message.
- `atproto_events_processed_total` — counter. Throughput + restart detection.

**Slow-poll (PLC):**
- `atproto_plc_operations_total{operation}` — counter (allowlisted PLC op types + `other`).
- `atproto_federation_pds_count` — gauge. Distinct PDS endpoints seen (single number, never labeled by host).

**Bounded top-N:**
- `atproto_top_domains{domain}` — gauge, top N≈20 linked domains over a rolling window, refreshed on an interval; falling-out series actively deleted.
- `atproto_top_hashtags{hashtag}` — gauge, same engine and guarantees.

**Self-observability (RED):**
- `atproto_exporter_ws_connected` — gauge 0/1
- `atproto_exporter_ws_reconnects_total` — counter
- `atproto_exporter_plc_poll_errors_total` — counter
- `atproto_exporter_plc_poll_duration_seconds` — histogram

**Consumer note (README):** counters reset on restart; every panel uses
`rate()`/`increase()`.

## Configuration (flags + env)

Every flag has an env-var equivalent (`ATPROTO_<UPPER_SNAKE>`) and a sane default so it
runs with zero flags. At minimum:

| Flag | Env | Default |
|---|---|---|
| `-listen` | `ATPROTO_LISTEN` | `:9200` |
| `-jetstream-host` | `ATPROTO_JETSTREAM_HOST` | `jetstream1.us-east.bsky.network` |
| `-jetstream-failover` | `ATPROTO_JETSTREAM_FAILOVER` | the other three public hosts |
| `-jetstream-zstd` | `ATPROTO_JETSTREAM_ZSTD` | `true` |
| `-collections` | `ATPROTO_COLLECTIONS` | post,like,repost,follow,block,profile,listitem,… |
| `-langs` | `ATPROTO_LANGS` | ~40 ISO-639 codes |
| `-topn` | `ATPROTO_TOPN` | `20` |
| `-topn-window` | `ATPROTO_TOPN_WINDOW` | `1h` |
| `-topn-refresh` | `ATPROTO_TOPN_REFRESH` | `15s` |
| `-plc-base-url` | `ATPROTO_PLC_BASE_URL` | `https://plc.directory` |
| `-plc-interval` | `ATPROTO_PLC_INTERVAL` | `30s` |
| `-cursor-dir` | `ATPROTO_CURSOR_DIR` | `./data` |
| `-log-level` | `ATPROTO_LOG_LEVEL` | `info` |
| `-cursor-rewind` | `ATPROTO_CURSOR_REWIND` | `5s` |

## Testing Strategy

**Framework:** stdlib `testing`, table-driven. Run with `-race -cover`.

**Unit (pure logic — TDD, write tests first):**
- `normalize`: collection allowlist bucketing (known → self, unknown → `other`); language normalization (case, region/script stripping, empty, non-allowlisted → `other`/`unknown`).
- `topn`: min-heap correctness, top-N selection, rolling-window expiry, **stale-series eviction** (a key that leaves the top set is deleted exactly once).
- `cursor`: read/write/round-trip, missing-file default, atomic write (no torn/partial cursor), rewind arithmetic.
- `plc`: op counting by type, distinct-PDS accumulation, cursor advance.
- `metrics`: label values stay within the allowlist under adversarial input (fuzz-ish table with junk NSIDs/langs); cardinality of top-N vectors never exceeds N.

**Integration (one, no live network):**
- Replay a recorded Jetstream sample fixture from `testdata/` through the full event pipeline; assert resulting metric values via `prometheus/testutil`. Also assert **idempotency**: replaying an overlapping (rewound) segment does not double-count where it must not, and metric deltas match expectations.

**Graceful degradation:** a test that forces PLC polling to fail and asserts event
metrics keep advancing (and vice versa).

**No live network in any test.**

## Boundaries

**Always:**
- Enforce allowlist + `other` bucket on every user-controlled label, always.
- `gofmt` + `go vet` clean; `go test -race ./...` green before commit.
- Context-aware shutdown; both source loops survive the other's failure (graceful degradation).
- Idempotent event processing (cursor rewind must be safe).
- Persist cursors with atomic writes; rewind a few seconds on resume.
- Structured logs only; aggregate numbers only.

**Ask first:**
- Adding any dependency beyond those listed in Tech Stack.
- Adding any new metric or label (must pass the cardinality review).
- Changing the metrics contract (metric names, label sets) — it's a public interface.
- Creating a new git repository or changing repo visibility.
- Publishing/sharing anything to an external service.

**Never:**
- Emit a handle, DID, IP, or record key as a label or in a log line — anywhere.
- Add per-account or per-user analytics, or any panel/metric that ranks individuals.
- Introduce a database, relay, AppView, PDS, or always-on indexing infrastructure.
- Store raw post content, archive records, or add full-text search.
- Use any live network call in a test.
- Read an absolute counter value in a dashboard panel as cumulative-since-genesis (use `rate()`/`increase()`).

## Deliverables

- Go exporter (single binary) meeting the metrics contract.
- `deploy/docker-compose.yml` standing up exporter + Prometheus + Grafana, dashboard auto-provisioned.
- Grafana dashboard JSON (importable, Prometheus datasource variable) with panels: events/sec by collection (stacked), posts/sec by top languages, account-growth rate (PLC), federation PDS count (stat), top linked domains (bar gauge), top hashtags (bar gauge), firehose lag (stat + timeseries), and an exporter-health row (WS connected, reconnects, PLC poll errors, poll duration).
- README: what it does, data sources, docker-compose run, cardinality rules, and an explicit **Known Limitations** section (counter resets, top-N approximation, lang-field quality, Jetstream-vs-full-firehose caveats).
- Two ADRs: `0001-cardinality-strategy`, `0002-cursor-and-replay-model`.
- Tests as described above.

## Success Criteria

1. `go build ./...` and `go test -race ./...` pass; `go vet` and `gofmt -l` clean.
2. `go run ./cmd/atproto-exporter` runs with **zero flags**, connects to Jetstream, tails PLC, and serves `/metrics` + `/healthz` (200).
3. `docker compose -f deploy/docker-compose.yml up` brings up all three services; Grafana shows the dashboard with live data, no manual import.
4. Every exported series conforms to the metrics contract; adversarial junk NSIDs/langs land in `other`; top-N vectors never exceed N series and evict correctly (verified by test).
5. Killing the process and restarting resumes from the persisted cursor (rewound) with no metric corruption; replay integration test proves idempotency.
6. Forcing PLC failure leaves event metrics flowing, and vice versa (test-verified).
7. No handle/DID/IP/record-key appears in any label or log line (test-verified for labels; code-reviewed for logs).
8. README + both ADRs present and accurate.

## Resolved Decisions

1. **Zstd dictionary** — embed the published Jetstream zstd dictionary via `//go:embed` from a vendored copy in the repo. A `make update-zstd-dict` target refreshes it (documented in the README) for when Bluesky rotates it.
2. **Post language** — read the post record's `langs[]` and take the **first** tag, then normalize + bucket it.
3. **Top domains** — extract domains **only** from structured link data: post `facets` link features and embedded external-link cards. **No** URL scraping of post text.
4. **PDS count** — `atproto_federation_pds_count` is derived from PLC operations' `services.atproto_pds.endpoint` (distinct endpoints), not Jetstream.
