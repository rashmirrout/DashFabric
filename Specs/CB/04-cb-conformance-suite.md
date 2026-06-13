# CB Conformance Suite

> **Status:** Draft v1
> **Audience:** Vendor CB implementers; CB SDK maintainers; FM core
> reviewers verifying compatibility.

This is the test set every CB implementation **must pass** before it can
be considered FM-compatible. Tests are organized by surface; mandatory
tests are gating, optional tests are encouraged.

## 1. How to run

```bash
cb-conformance \
  --target=acme-cb.example.com:7443 \
  --tls-cert=client.crt --tls-key=client.key \
  --topics=devices,nics,vnets,mappings,acls,routes,ha \
  --report=./report.html
```

The harness drives the CB through a deterministic sequence of
operations and checks observed behavior against the contract.

## 2. Mandatory tests (gating)

| ID | Name | What it verifies |
|----|------|-------------------|
| **T1** | Init / Health | `Init()` returns within 5s; `Health()` reports `SERVING` after warmup; version negotiation accepts `v1`. |
| **T2** | Subscribe basic | Open a stream on a wildcard pattern; vendor publishes 100 events into CP; FM-side stream receives them all in order with valid watermarks. |
| **T3** | Watermark resume | Receive 50 events, close stream, save watermark `W`. Reopen with `from_watermark=W`; receive only events with watermark `> W`. |
| **T4** | Get returns latest | After UPSERT then UPSERT, `Get(key)` returns the second value (compacted semantics). |
| **T5** | List returns full snapshot | After 100 UPSERTs across 100 keys, `List(topic)` streams 100 events. |
| **T6** | Resync framing | `Resync(topic_pattern)` emits `RESYNC_BEGIN` first, then events, then `RESYNC_END` last. No events outside the brackets. |
| **T7** | FM Publish — delivery ack | FM publishes a `DeliveryAck` event; `PublishResult.ok=true`; subsequent `Get` on `…/ack/delivery` returns the event with matching `content_hash`. |
| **T8** | FM Publish — state ack | FM publishes a `StateAck` event to a compacted ack topic; `Get` returns latest; subsequent overwrite reflects new value, not old. |
| **T9** | Compacted topic semantics | UPSERT key K with v1, then v2. `List` returns only v2 (not both); `Subscribe` joining after the second UPSERT receives only v2. |
| **T10** | Append-log topic semantics | Publish 100 ack events to `…/ack/delivery`; subscriber from offset 0 receives exactly 100 events in publish order. |
| **T11** | Idempotency on republish | Publish same `(topic, key, content_hash)` twice. Compacted: no duplicate notification. Append-log: deduped within `cb.publish.dedup.window`. |
| **T12** | Subscribe / Publish independence | Stall a Subscribe consumer (don't read for 10s). Publish must continue to succeed. After consumer resumes (within `cb.subscribe.slow_drop_after`), it either catches up or is dropped with `FM_TOO_SLOW` (no other failure mode). |

A CB that fails any of T1–T12 is **not** FM-compatible.

## 3. Optional / extended tests

These verify good citizenship but are not gating. Score is reported
in the conformance report.

| ID | Name | What it verifies |
|----|------|-------------------|
| T13 | Pattern matching | `Subscribe("/dashfabric/v1/config/vnets/*")` matches all VNET keys; `**` matches recursively. |
| T14 | Tombstone visibility | DELETE on key K produces a tombstone event observable by subscribers within `cb.tombstone.retention`. |
| T15 | High throughput | Sustain ≥ 50k events/sec for 60s without dropping or lagging > 1s. |
| T16 | Resync large topic | `Resync` of a 1M-key topic completes in < 30s. |
| T17 | Crash recovery — durable | Kill -9 the CB process; restart; `Subscribe(from_watermark=W)` resumes correctly without `RESYNC_NEEDED` (only if `cb.store.backend != memory`). |
| T18 | Crash recovery — ephemeral | Same scenario with memory backend; CB returns `RESYNC_NEEDED` and accepts a `Resync` cleanly. |
| T19 | Vendor CP partition | Disconnect CB from vendor CP for 30s. `Health()` reports `DEGRADED` with reason. On reconnect, CB catches up and resumes streams. |
| T20 | Backpressure ordering | Multiple parallel subscribers at different consumption rates; each receives events in topic-order independent of others. |
| T21 | Watermark monotonicity | Across 100k events, watermark per topic is strictly non-decreasing. |
| T22 | mTLS rejection | Connection with bad cert is rejected with `Unauthenticated`; valid cert succeeds. |
| T23 | Authz enforcement | FM cert without publish ACL on a topic is rejected with `PermissionDenied` on `Publish`. |
| T24 | Health detail accuracy | `Health()` `cp_side.last_event_ts` is within 5s of last received vendor event. |

## 4. Per-topic conformance

For each topic the CB carries, additional checks:

| Topic | Extra check |
|-------|-------------|
| `nics` | Event payload validates against `Nic` proto; `eni_id` matches `ENI_<DPU>_<MAC>`. |
| `vnets` | Payload includes `vni`, `address_spaces`. |
| `mappings` | Single event carries the **full** mapping table for the VNET (T-MAP-1: emitting per-prefix events fails this). |
| `acls` | Rules carry priority and ordered correctly. |
| `routes` | Route entries reference valid prefix syntax. |
| `ha` | `pairing_state` is one of the documented enum values. |

## 5. Pass / fail criteria

- **Pass:** all T1–T12 pass; per-topic checks pass for every carried topic.
- **Pass with warnings:** mandatory pass; some optional tests fail. CB
  is FM-compatible but should improve.
- **Fail:** any mandatory test fails. Cannot be deployed in production
  with FM.

## 6. Report format

The harness emits HTML + JSON. Required fields per test:

```json
{
  "test_id": "T7",
  "name": "FM Publish — delivery ack",
  "status": "PASS",
  "duration_ms": 142,
  "details": "...",
  "events_observed": 1
}
```

Aggregate:

```json
{
  "cb_version": "acme-cb 1.2.3",
  "cb_target": "acme-cb.example.com:7443",
  "topics_carried": ["devices","nics","vnets","mappings","acls"],
  "total": 24,
  "passed": 23,
  "failed": 0,
  "warnings": 1,
  "verdict": "PASS_WITH_WARNINGS"
}
```

## 7. Continuous conformance

Vendor SHOULD run the conformance suite:

- On every CB release (CI gate).
- Nightly against staging deployment.
- After any FM-side proto bump (the harness pulls the latest
  `cb_fm_protos`).

DashFabric maintains the harness in lock-step with the proto repo;
proto changes that would break vendors will bump `cb_fm_protos`'s major
version (Protocol 6 — every default a knob, including the proto major).

## 8. References

- `02-cb-low-level-design-lld.md` — internals being verified.
- `Specs/cb_fm_protos/` — the contract under test.
- Conformance harness source: (to be added under `tools/cb-conformance/`).
