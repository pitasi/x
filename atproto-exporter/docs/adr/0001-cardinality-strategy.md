# ADR 0001: Bounded-cardinality strategy

## Status

Accepted

## Context

The exporter derives metrics from a public, permissionless network. Anyone can
mint new lexicons (NSIDs), handles, domains, and hashtags. Prometheus label
values map 1:1 to time series, so any label fed a user-controlled value has
**unbounded cardinality** — a denial-of-service on our own TSDB and a cost/scale
problem in Mimir. The dashboard is also public, so we must not build a
surveillance surface: no metric may expose or rank an individual account.

## Decision

No label may take an unbounded, user-controlled value. Concretely:

1. **Allowlist + `other` bucket** for every user-supplied dimension.
   - `collection` (NSID) → allowlisted NSIDs, else `other` (`internal/normalize`).
   - `lang` → ~40 allowlisted ISO-639 codes, else `other`; empty → `unknown`.
   - PLC `operation` → `create | update | tombstone | other`.
2. **Never label by identity or free-form value.** No DID, handle, IP, or record
   key is ever a label — or a log field. `atproto_federation_pds_count` is a
   single number, never labeled by host.
3. **Bounded top-N for ranked values.** `atproto_top_domains` and
   `atproto_top_hashtags` are computed in-process over a rolling window
   (`internal/topn`) and only the top N≈20 are exported. Series that fall out of
   the top set are **actively deleted** (`metrics.TopNGauge.Sync`) so churn never
   accumulates. Cardinality is therefore hard-capped at N per metric — verified
   by a test that fails if a top-N vector ever exceeds N.
4. **Normalization happens at the source.** The pipeline normalizes before
   touching a metric; the metrics façade only accepts already-normalized values.

## Consequences

- Cardinality is bounded by construction and enforced by tests, independent of
  what the network throws at us.
- Domain/hashtag rankings are *approximate* (in-memory, rolling window, top-N
  only) — acceptable for a trends dashboard, and the only safe option without a
  database.
- Aggregates only: the dashboard can show "what's happening on the network" but
  can never single out or rank a person.
