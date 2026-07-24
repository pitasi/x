# atproto-exporter

A single, lightweight Prometheus exporter for live, **network-wide** analytics of
the AT Protocol ("atmosphere", i.e. Bluesky and the wider network). It consumes
public, unauthenticated sources, holds a little state in memory, and serves
`/metrics`. Point your Prometheus/Mimir at it and visualize with the bundled
Grafana dashboard.

No relay, no AppView, no PDS, no database. No per-account analytics — **aggregates
only**. It is explicitly *not* a surveillance surface.

## What it does

- Streams the **Jetstream** firehose (one WebSocket) and derives event, post, and
  lag metrics in real time.
- Tails the **plc.directory export log** to count network-wide identity
  operations and derive the size of the federation (distinct PDS endpoints).
- Computes bounded **top-N** linked domains and hashtags over a rolling window.
- Exposes everything as bounded-cardinality Prometheus metrics, plus RED-style
  self-observability for the exporter itself.

## Data sources (all public, no auth)

| Source | What we read | How |
|---|---|---|
| **Jetstream** | firehose events (commits, identity, account) | `wss://<host>/subscribe`, time-based cursor (`time_us`), optional zstd |
| **plc.directory** | identity operations, PDS endpoints | `GET {base}/export?after=<cursor>`, NDJSON, `after` cursor |

Public Jetstream hosts (primary + failover): `jetstream1.us-east.bsky.network`,
`jetstream2.us-east.bsky.network`, `jetstream1.us-west.bsky.network`,
`jetstream2.us-west.bsky.network`.

## Quick start

Zero-flag run (writes cursors to `./data`):

```sh
go run ./cmd/atproto-exporter
# metrics:  http://localhost:9200/metrics
# health:   http://localhost:9200/healthz
```

### Full local stack (exporter + Prometheus + Grafana)

```sh
cd deploy
docker compose up --build
```

- Grafana: http://localhost:3000 (anonymous admin; dashboard **AT Protocol
  Network** is auto-provisioned)
- Prometheus: http://localhost:9090
- Exporter: http://localhost:9200/metrics

The exporter is an ordinary scrape target, so you can also point your own
Prometheus/Mimir at `:9200` and import `deploy/grafana/dashboards/atproto.json`.

## Metrics contract

Counters reset on restart — **every dashboard panel uses `rate()`/`increase()`**;
never read a counter as a cumulative-since-genesis value.

| Metric | Type | Labels | Notes |
|---|---|---|---|
| `atproto_events_total` | counter | `kind`, `collection`, `operation` | `collection` allowlisted → else `other` |
| `atproto_posts_total` | counter | `lang` | allowlisted ISO code → else `other`/`unknown` |
| `atproto_firehose_lag_seconds` | gauge | — | `now - time_us` of last message |
| `atproto_events_processed_total` | counter | — | throughput + restart detection |
| `atproto_plc_operations_total` | counter | `operation` | `create`/`update`/`tombstone`/`other` |
| `atproto_federation_pds_count` | gauge | — | distinct PDS endpoints (never labeled by host) |
| `atproto_top_domains` | gauge | `domain` | top-N over rolling window; stale series deleted |
| `atproto_top_hashtags` | gauge | `hashtag` | same mechanism |
| `atproto_exporter_ws_connected` | gauge | — | 0/1 |
| `atproto_exporter_ws_reconnects_total` | counter | — | |
| `atproto_exporter_plc_poll_errors_total` | counter | — | |
| `atproto_exporter_plc_poll_duration_seconds` | histogram | — | |

### Cardinality rules (the one hard constraint)

No label ever takes an unbounded, user-controlled value. Allowlist + `other`
bucket for `collection`, `lang`, and PLC `operation`; top-N with active eviction
for domains/hashtags (hard-capped at N); **never** a DID, handle, IP, or record
key as a label or a log field. See [ADR 0001](docs/adr/0001-cardinality-strategy.md).

## Configuration

Every flag has an `ATPROTO_<UPPER_SNAKE>` env fallback and a sane default, so it
runs with zero flags.

| Flag | Env | Default |
|---|---|---|
| `-listen` | `ATPROTO_LISTEN` | `:9200` |
| `-jetstream-host` | `ATPROTO_JETSTREAM_HOST` | `jetstream1.us-east.bsky.network` |
| `-jetstream-failover` | `ATPROTO_JETSTREAM_FAILOVER` | the other three public hosts |
| `-jetstream-zstd` | `ATPROTO_JETSTREAM_ZSTD` | `true` |
| `-collections` | `ATPROTO_COLLECTIONS` | common `app.bsky.*` NSIDs |
| `-langs` | `ATPROTO_LANGS` | ~40 ISO-639 codes |
| `-topn` | `ATPROTO_TOPN` | `20` |
| `-topn-window` | `ATPROTO_TOPN_WINDOW` | `1h` |
| `-topn-refresh` | `ATPROTO_TOPN_REFRESH` | `15s` |
| `-plc-base-url` | `ATPROTO_PLC_BASE_URL` | `https://plc.directory` |
| `-plc-interval` | `ATPROTO_PLC_INTERVAL` | `30s` |
| `-cursor-dir` | `ATPROTO_CURSOR_DIR` | `./data` |
| `-cursor-rewind` | `ATPROTO_CURSOR_REWIND` | `5s` |
| `-log-level` | `ATPROTO_LOG_LEVEL` | `info` |

## Development

```sh
make build          # build the binary
make race           # go test -race -cover ./...
make lint           # go vet + gofmt check
make docker         # build the container image
make compose        # bring up the full local stack
make update-zstd-dict  # refresh the embedded Jetstream zstd dictionary
```

Tests use no live network: the Jetstream WS is faked via an injected dialer, PLC
via `httptest`, and there is a fixture-replay integration test
(`internal/jetstream/integration_test.go`) that also proves rewind idempotency
and graceful degradation.

## Design

- [ADR 0001 — Bounded-cardinality strategy](docs/adr/0001-cardinality-strategy.md)
- [ADR 0002 — Cursor persistence and replay model](docs/adr/0002-cursor-and-replay-model.md)

## Known limitations

- **Counter resets.** All counters reset when the process restarts. Panels must
  use `rate()`/`increase()`; a raw counter value is meaningless across restarts.
- **Top-N is approximate.** Domains/hashtags are counted in-memory over a rolling
  window and only the top N are exported. It is a trends signal, not an exact
  leaderboard, and it resets on restart.
- **Language field quality.** `langs` is user-supplied and dirty. We take the
  first tag, strip region/script subtags, lowercase, and bucket anything outside
  the allowlist to `other` (empty → `unknown`). Counts approximate real language
  distribution.
- **Jetstream vs. full firehose.** Jetstream is a convenient, filtered JSON view
  of the firehose, not the authoritative `com.atproto.sync` CAR stream. It can
  drop under load and only carries what Bluesky's instances relay. Numbers are
  network *activity as seen via Jetstream*, not a cryptographically complete
  count.
- **PLC-derived federation size** counts distinct PDS endpoints observed in the
  export log since startup; it grows as new endpoints appear and does not shrink.
- **Account event mapping.** Jetstream `account` events are mapped to
  `create`/`delete` by their `active` flag — an approximation of account
  lifecycle, not an exact create/delete ledger.

## Boundaries

No database, no relay/AppView/PDS, no record archival or full-text search, no PII
(no handles/DIDs/IPs/record keys as labels or logs), and no metric or panel that
exposes or ranks an individual account.
