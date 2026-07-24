# Implementation Plan: atproto-exporter

## Overview

Build a single Go binary that consumes public AT Protocol data (Jetstream WebSocket +
plc.directory export log) and exposes bounded-cardinality Prometheus metrics for a public
Grafana dashboard, plus a `docker compose` stack (exporter + Prometheus + Grafana),
README, ADRs, and tests. See `SPEC.md` for the full contract. Greenfield project;
module `anto.pt/x/atproto-exporter`, Go 1.26.

## Architecture Decisions

- **Pure logic isolated from I/O.** `normalize`, `topn`, `cursor` are pure packages with
  no network/goroutine dependencies, built and tested first (TDD). This makes the
  cardinality contract and top-N eviction trivially testable and is the backbone of the
  dependency graph.
- **Pipeline before transport.** The Jetstream *event pipeline* (bytes → metrics) is built
  and fixture-tested before the *WebSocket consumer* (the network transport). This lets the
  integration test replay a recorded fixture with no live network and proves idempotency
  on cursor rewind independently of reconnect logic.
- **One shared metrics registry** owned by the `metrics` package; each source (Jetstream,
  PLC) gets a typed façade so a source can fail without touching the other's series
  (graceful degradation).
- **zstd dictionary embedded** via `//go:embed`; refreshed by a `make update-zstd-dict`
  target. Plain-JSON is the fallback wire format.
- **Vertical slices after the foundation.** Foundation (config + pure logic + metrics) is
  necessarily shared; above it, the Jetstream slice and the PLC slice are independent
  end-to-end paths that can be built and verified separately.

## Dependency Graph

```
config (T1)
  ├── normalize (T2) ──┐
  ├── topn      (T3) ──┼──→ metrics (T5) ──→ event pipeline (T6) ──→ WS consumer (T7) ──┐
  ├── cursor    (T4) ──┘                            │                                    │
  │                                                 └──────────────────┐                 ├─→ main wiring (T9) ─→ integration+degradation tests (T10)
  └──────────────────────────────────────→ PLC poll loop (T8) ────────┘                 │
                                                                                         │
deploy stack (T11), dashboard JSON (T12), docs+Makefile (T13)  ──────── depend on ───────┘
```

Build bottom-up. T2/T3/T4 are mutually independent (parallelizable). T11/T12/T13 are
independent of each other once T9/T10 are green.

## Task List

### Phase 1: Foundation

- **Task 1** — Module + config + runnable HTTP skeleton
- **Task 2** — `normalize` package (TDD)
- **Task 3** — `topn` package (TDD)
- **Task 4** — `cursor` package (TDD)

**Checkpoint A:** `go build ./...`, `go test -race ./...`, `go vet`, `gofmt -l` all clean;
binary serves `/healthz` 200 and an (empty) `/metrics`. Review with human.

### Phase 2: Jetstream slice

- **Task 5** — `metrics` package (all collectors + registry + TopNGauge sync)
- **Task 6** — Jetstream event pipeline (bytes → metrics), cursor persist + rewind
- **Task 7** — Jetstream WS consumer (connect, zstd, reconnect backoff+jitter, failover)

**Checkpoint B:** pipeline unit tests green; running the binary connects to a live
Jetstream host and `atproto_events_total` / `atproto_posts_total` advance;
`atproto_exporter_ws_connected` = 1. Review with human.

### Phase 3: PLC slice

- **Task 8** — PLC poll loop (fetch export, count ops, distinct-PDS set, cursor, self-metrics)

**Checkpoint C:** PLC unit tests green; running the binary populates
`atproto_plc_operations_total` and `atproto_federation_pds_count`.

### Phase 4: Integration & wiring

- **Task 9** — `main` wiring: goroutines, context shutdown, signals, graceful degradation
- **Task 10** — Integration test (fixture replay + idempotency) + graceful-degradation test

**Checkpoint D:** `go test -race ./...` green including integration; SIGINT shuts down
cleanly; restart resumes from persisted cursor. Review with human.

### Phase 5: Deploy & docs

- **Task 11** — Dockerfile + docker-compose (exporter + Prometheus + Grafana) + scrape config + provisioning
- **Task 12** — Grafana dashboard JSON (all panels + Prometheus datasource variable)
- **Task 13** — README + ADR 0001/0002 + Makefile (`update-zstd-dict`)

**Checkpoint E (Complete):** `docker compose up` brings up all three services; Grafana
shows the dashboard with live data, no manual import. All `SPEC.md` success criteria met.
Ready for review.

---

## Task Detail

### Task 1: Module + config + runnable HTTP skeleton
**Description:** Init the Go module, create `internal/config` (flags + `ATPROTO_*` env
fallbacks + defaults + allowlist parsing per SPEC config table), and `cmd/atproto-exporter/main.go`
that builds config, sets up a `slog` JSON logger, and serves `/healthz` (200) and `/metrics`
(prometheus handler, empty registry for now) on the configured listen address.
**Acceptance criteria:**
- [ ] `go run ./cmd/atproto-exporter` with zero flags starts and serves `/healthz` → 200 and `/metrics` → 200.
- [ ] Each flag has an `ATPROTO_*` env fallback; defaults match the SPEC config table.
- [ ] Config parse errors exit non-zero with a clear message; log level is honored.
**Verification:** `go test ./internal/config/...`; `go vet ./...`; `curl :9200/healthz`.
**Dependencies:** None. **Files:** `go.mod`, `cmd/atproto-exporter/main.go`, `internal/config/config.go`, `internal/config/config_test.go`. **Scope:** M.

### Task 2: `normalize` package (TDD)
**Description:** Pure collection-allowlist bucketing and language normalization.
**Acceptance criteria:**
- [ ] Known NSID → itself; unknown → `other`.
- [ ] Language: lowercased, region/script subtags dropped (`en-US`→`en`); empty → `unknown`; non-allowlisted → `other`.
- [ ] Allowlist built once from config; lookups O(1).
**Verification:** `go test -race ./internal/normalize/...` (table-driven, incl. junk input).
**Dependencies:** T1 (config types). **Files:** `internal/normalize/normalize.go`, `_test.go`. **Scope:** S.

### Task 3: `topn` package (TDD)
**Description:** Generic bounded min-heap top-N over a rolling window with explicit stale
eviction (the reusable engine for domains and hashtags).
**Acceptance criteria:**
- [ ] Correctly returns the top-N by count; ties handled deterministically.
- [ ] Rolling-window entries expire; counts decay out of the window.
- [ ] Exposes a stable diff (added/removed keys) so callers can delete evicted series exactly once.
**Verification:** `go test -race ./internal/topn/...` including window-expiry and eviction cases.
**Dependencies:** None. **Files:** `internal/topn/topn.go`, `_test.go`. **Scope:** M.

### Task 4: `cursor` package (TDD)
**Description:** Persist/restore source cursors with atomic writes and rewind arithmetic.
**Acceptance criteria:**
- [ ] Write→read round-trips a cursor value; missing file returns a documented default.
- [ ] Writes are atomic (temp file + rename); no torn/partial cursor on crash.
- [ ] Rewind subtracts the configured delta (clamped ≥ 0) from a time-based cursor.
**Verification:** `go test -race ./internal/cursor/...`.
**Dependencies:** T1. **Files:** `internal/cursor/cursor.go`, `_test.go`. **Scope:** S.

### Task 5: `metrics` package
**Description:** Define every collector from the SPEC contract, own the registry, and wrap
top-N vectors in a `TopNGauge.Sync` that deletes fallen-out series. Provide typed façades
for the Jetstream and PLC sources.
**Acceptance criteria:**
- [ ] All contract metrics registered with exact names/labels from SPEC.
- [ ] `TopNGauge.Sync` sets current top-N and deletes any previously-present key now absent; vector cardinality never exceeds N (test-verified).
- [ ] Adversarial label values are impossible: façade methods only accept already-normalized labels.
**Verification:** `go test -race ./internal/metrics/...` using `prometheus/testutil`.
**Dependencies:** T3. **Files:** `internal/metrics/metrics.go`, `_test.go`. **Scope:** M.

### Task 6: Jetstream event pipeline
**Description:** Pure-ish consumer: given decoded Jetstream messages, update
`atproto_events_total` (kind/collection/operation), `atproto_posts_total{lang}` (first
`langs[]` tag), `atproto_firehose_lag_seconds`, `atproto_events_processed_total`; feed
domains (from facets link features + external embed cards) and hashtags into `topn`;
persist the `time_us` cursor. Idempotent per event so rewind is safe.
**Acceptance criteria:**
- [ ] Each message maps to the correct metric increments; unknown collections/langs bucket to `other`.
- [ ] Domains/hashtags extracted only from structured facets/embeds (no text scraping).
- [ ] Processing is idempotent w.r.t. replayed overlapping segments where required; cursor advances monotonically.
**Verification:** `go test -race ./internal/jetstream/...` (pipeline-level, fixture-driven).
**Dependencies:** T2, T3, T5, T4. **Files:** `internal/jetstream/pipeline.go`, `_test.go`, `testdata/jetstream_sample.jsonl`. **Scope:** M.

### Task 7: Jetstream WS consumer
**Description:** Network transport around the pipeline: connect to `wss://host/subscribe`
with cursor + collection filters, optional zstd (embedded dictionary), reconnect with
exponential backoff + jitter, failover across the host list; drive
`atproto_exporter_ws_connected` and `atproto_exporter_ws_reconnects_total`.
**Acceptance criteria:**
- [ ] Connects, streams messages into the pipeline; sets `ws_connected`=1 while up, 0 on disconnect.
- [ ] Reconnects with bounded exponential backoff + jitter; rotates to failover hosts; increments reconnect counter.
- [ ] zstd decode works against the embedded dictionary; falls back to plain JSON when disabled.
**Verification:** `go test -race ./internal/jetstream/...` (backoff/failover unit tests with a fake dialer); manual live-connect smoke check.
**Dependencies:** T6. **Files:** `internal/jetstream/consumer.go`, `_test.go`, `internal/jetstream/zstd_dict.bin` (+embed). **Scope:** M.

### Task 8: PLC poll loop
**Description:** Goroutine polling `{base}/export?after=<cursor>` on an interval: parse NDJSON
ops, count by type into `atproto_plc_operations_total{operation}`, accumulate distinct
`services.atproto_pds.endpoint` into `atproto_federation_pds_count`, persist its own cursor,
and record `atproto_exporter_plc_poll_errors_total` / `atproto_exporter_plc_poll_duration_seconds`.
**Acceptance criteria:**
- [ ] Ops counted by allowlisted type (+`other`); cursor advances by last op timestamp.
- [ ] Distinct PDS endpoints accumulated into the gauge (never labeled by host).
- [ ] Poll errors increment the error counter and are retried next tick without crashing.
**Verification:** `go test -race ./internal/plc/...` (fixture NDJSON via `httptest`).
**Dependencies:** T4, T5. **Files:** `internal/plc/plc.go`, `_test.go`, `testdata/plc_sample.jsonl`. **Scope:** M.

### Task 9: `main` wiring & graceful degradation
**Description:** Wire config → logger → registry → Jetstream consumer + PLC loop as
goroutines under a root context; handle SIGINT/SIGTERM for clean shutdown; ensure one
source failing (panic/error) does not stop the other.
**Acceptance criteria:**
- [ ] Both loops start; SIGINT/SIGTERM cancels context and both stop within a bounded grace period.
- [ ] A forced failure in one source leaves the other's metrics advancing.
- [ ] `/healthz` reflects liveness; process exits 0 on clean shutdown.
**Verification:** `go test -race ./...`; manual SIGINT check; restart resumes from cursor.
**Dependencies:** T7, T8. **Files:** `cmd/atproto-exporter/main.go`. **Scope:** M.

### Task 10: Integration & degradation tests
**Description:** End-to-end fixture replay through the full Jetstream pipeline asserting
metric values via `prometheus/testutil`, plus an idempotency assertion on a rewound
overlapping replay, plus a graceful-degradation test (PLC fails → events keep flowing).
**Acceptance criteria:**
- [ ] Replaying `testdata/jetstream_sample.jsonl` yields the expected metric values.
- [ ] Replaying an overlapping (rewound) segment does not corrupt counts beyond expected deltas.
- [ ] Degradation test passes with no live network.
**Verification:** `go test -race ./...`.
**Dependencies:** T6, T8, T9. **Files:** `internal/jetstream/integration_test.go`, degradation test. **Scope:** M.

### Task 11: Deploy stack
**Description:** Dockerfile (static binary, distroless/scratch) + `deploy/docker-compose.yml`
running exporter + Prometheus + Grafana, with `prometheus/prometheus.yml` scraping the
exporter and Grafana provisioning (datasource + dashboard).
**Acceptance criteria:**
- [ ] `docker build` produces a working image; `docker compose up` starts all three services.
- [ ] Prometheus scrapes the exporter; Grafana auto-loads the datasource + dashboard.
**Verification:** `docker compose -f deploy/docker-compose.yml up --build`; check targets in Prometheus, dashboard in Grafana.
**Dependencies:** T9. **Files:** `Dockerfile`, `deploy/docker-compose.yml`, `deploy/prometheus/prometheus.yml`, `deploy/grafana/provisioning/**`. **Scope:** M.

### Task 12: Grafana dashboard JSON
**Description:** Importable dashboard with a Prometheus datasource variable and all SPEC
panels: events/sec by collection (stacked), posts/sec by top languages, account-growth rate
(PLC), federation PDS count (stat), top domains (bar gauge), top hashtags (bar gauge),
firehose lag (stat + timeseries), exporter-health row (WS connected, reconnects, PLC poll
errors, poll duration). All rate/increase-based per the counter-reset note.
**Acceptance criteria:**
- [ ] Dashboard imports cleanly against a `$datasource` Prometheus variable.
- [ ] Every panel query uses `rate()`/`increase()` for counters; no raw-counter panels.
**Verification:** import into the provisioned Grafana; panels render with live data.
**Dependencies:** T9 (metric names finalized). **Files:** `deploy/grafana/dashboards/atproto.json`. **Scope:** M.

### Task 13: Docs & Makefile
**Description:** README (what it does, data sources, docker-compose run, cardinality rules,
Known Limitations), ADR 0001 (cardinality strategy), ADR 0002 (cursor/replay model), and a
Makefile with build/test/vet plus `update-zstd-dict`.
**Acceptance criteria:**
- [ ] README covers all required sections incl. explicit Known Limitations (counter resets, top-N approximation, lang-field quality, Jetstream-vs-full-firehose).
- [ ] Both ADRs present and accurate; `make update-zstd-dict` refreshes the embedded dictionary.
**Verification:** manual read-through; `make build && make test` succeed.
**Dependencies:** T9. **Files:** `README.md`, `docs/adr/0001-cardinality-strategy.md`, `docs/adr/0002-cursor-and-replay-model.md`, `Makefile`. **Scope:** M.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Jetstream message schema drift / undocumented fields | Med | Isolate parsing in the pipeline; drive tests from a recorded fixture; tolerate unknown fields, bucket unknowns to `other`. |
| zstd dictionary rotation by Bluesky breaks decode | Med | Embed dict + `make update-zstd-dict`; plain-JSON fallback (`-jetstream-zstd=false`) keeps the exporter working. |
| Unbounded cardinality leak (the core hazard) | High | Allowlist+`other` enforced in `normalize`/façade; `metrics` test asserts top-N vectors never exceed N and evict; code review checks no handle/DID/IP/rkey in labels or logs. |
| PLC export pagination/rate limits | Low-Med | Persisted `after=` cursor + bounded `count`; poll errors retried next tick; errors counted, not fatal. |
| Cursor rewind double-counts non-idempotent metrics | Med | Keep event processing idempotent where required; integration test explicitly replays an overlapping rewound segment. |
| Live-network flakiness in tests | Low | Hard rule: no live network in tests; fake dialer for WS, `httptest` for PLC, fixtures for pipeline. |

## Open Questions

None outstanding — the four prior open questions are resolved in `SPEC.md` (Resolved Decisions).
Confirm the phase checkpoints are the right human-review gates before implementation starts.
