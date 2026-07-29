# ADR 0002: Cursor persistence and replay model

## Status

Accepted

## Context

Both data sources are resumable streams:

- **Jetstream** uses a time-based cursor (`time_us`, unix microseconds). On
  reconnect we want *gapless* resumption — we must not skip events during the
  reconnect window.
- **plc.directory** `/export` is tailed with an `after=<createdAt>` cursor.

We hold no database; state is in-memory plus small on-disk cursors. We must
survive restarts and reconnects without corrupting metrics.

## Decision

1. **Persist cursors to disk with atomic writes** (`internal/cursor`): write to a
   temp file in the same directory, fsync, then rename. A crash never leaves a
   torn cursor. Jetstream and PLC each own a cursor file under `cursor-dir`.
2. **Rewind on resume for gaplessness.** When (re)connecting to Jetstream we
   request `cursor = lastProcessed - rewind` (default 5s, clamped ≥ 0). This
   guarantees we re-receive the reconnect window rather than skipping it.
3. **Idempotent processing makes rewind safe.** The pipeline tracks the highest
   `time_us` it has processed and **skips any event with `time_us <= last`**.
   Combined with rewind, this yields gapless *and* exactly-once counting within a
   run: the overlapping segment is re-received (gapless) but not re-counted
   (idempotent). Proven by `TestIntegrationRewindIdempotent`.
4. **Cross-restart semantics.** Counters reset to zero on process restart (they
   are Prometheus counters), so re-counting a few seconds after a restart is
   irrelevant — dashboards use `rate()`/`increase()`, which handle resets. The
   persisted cursor simply avoids re-scanning large backlogs.
5. **Throttled persistence.** The Jetstream cursor is written every N processed
   events (default 100) rather than per-event, bounding disk I/O; the PLC cursor
   is written after each drained page.

## Consequences

- Safe restarts and reconnects with no gaps and no double-counting within a run.
- The rewind window is a small, tunable trade-off between reconnect gap-safety
  and replay volume.
- Because idempotency depends on monotonic `time_us`, out-of-order delivery would
  weaken it; Jetstream is ordered by `time_us`, so this holds in practice, and
  top-N (already approximate) tolerates the rare exception.
