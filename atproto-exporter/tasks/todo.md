# TODO: atproto-exporter

Ordered by dependency. Check off as completed. See `tasks/plan.md` for full task detail
and `SPEC.md` for the contract. Do not start until the plan is approved.

## Phase 1: Foundation
- [x] **T1** Module + config (flags/env/defaults) + runnable HTTP skeleton (`/healthz`, `/metrics`)
- [x] **T2** `normalize` package — collection bucketing + language normalization (TDD)
- [x] **T3** `topn` package — bounded min-heap + rolling window + eviction diff (TDD)
- [x] **T4** `cursor` package — atomic read/write/round-trip + rewind (TDD)

### ✅ Checkpoint A
- [ ] `go build ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean
- [ ] Binary serves `/healthz` 200 + empty `/metrics`
- [ ] **Human review**

## Phase 2: Jetstream slice
- [ ] **T5** `metrics` package — all collectors + registry + `TopNGauge.Sync` eviction
- [ ] **T6** Jetstream event pipeline (bytes → metrics) + cursor persist/rewind
- [ ] **T7** Jetstream WS consumer — connect, zstd (embedded dict), backoff+jitter, failover

### ✅ Checkpoint B
- [ ] Pipeline unit tests green
- [ ] Live connect: `events_total`/`posts_total` advance, `ws_connected`=1
- [ ] **Human review**

## Phase 3: PLC slice
- [ ] **T8** PLC poll loop — op counting + distinct-PDS gauge + cursor + self-metrics

### ✅ Checkpoint C
- [ ] PLC unit tests green
- [ ] `plc_operations_total` + `federation_pds_count` populate on live run

## Phase 4: Integration & wiring
- [ ] **T9** `main` wiring — goroutines, context shutdown, signals, graceful degradation
- [ ] **T10** Integration test (fixture replay + idempotency) + graceful-degradation test

### ✅ Checkpoint D
- [ ] `go test -race ./...` green incl. integration
- [ ] SIGINT shuts down cleanly; restart resumes from cursor
- [ ] **Human review**

## Phase 5: Deploy & docs
- [ ] **T11** Dockerfile + docker-compose (exporter + Prometheus + Grafana) + provisioning
- [ ] **T12** Grafana dashboard JSON (all panels + `$datasource` variable)
- [ ] **T13** README + ADR 0001/0002 + Makefile (`update-zstd-dict`)

### ✅ Checkpoint E (Complete)
- [ ] `docker compose up` → all three services; Grafana dashboard live, no manual import
- [ ] All `SPEC.md` success criteria met
- [ ] **Ready for review**
