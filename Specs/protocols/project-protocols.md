# DashFabric Project Protocols

The standing protocols for this project. These are *postures*, not
one-time decisions — every spec and design call is judged against them.

## Snapshot

- **What it is:** next-generation datacenter fleet management system for
  DASH-compliant appliances at hyperscale.
- **Scope:** DV (DPU-virtual), VM, and Container DASH appliance forms.
- **Goal:** **full DASH support** — VNETs, routing, ACLs, mappings, HA,
  metering — not partial coverage.
- **Scale target:** 10,000+ hosts per DC; 1M–10M ENIs in the large tier.
- **Stage:** architecture & design spec phase. No implementation yet.

## Protocol 1 — Always align with upstream DASH

**Rule:** Every DASH-derived concept must, in the doc that introduces it,
name the upstream DASH artifact it derives from. Where DashFabric extends
or envelopes the concept, the extension is labeled. Reference:
<https://github.com/sonic-net/DASH/tree/main>.

**Why:** DashFabric is a *consumer* of DASH, not a parallel author. If a
contributor reads a published proto and thinks the field set is something
*we* designed, we've done the wrong thing.

**How to apply:** "Upstream alignment" callout on every published-proto
doc, listing source SAI tables. Three buckets per field —
**upstream / envelope / synthetic**. Action enums get inline
`(DASH: <upstream-action-name>)`. See
`Specs/me-and-ai/upstream-dash-alignment.md`.

## Protocol 2 — Control plane is vendor-specific; FM is vendor-neutral

**Rule:** FM does not assume the orchestrator runs etcd, K8s, Kafka,
ZooKeeper, or any specific schema. Upstream events arrive via a
**plugin** delivering opaque payloads under well-known topic strings
with watermarks. The adapter parses; the plugin contract stays vendor-free.

**Why:** Different vendors run different control planes. Coupling FM to
one vendor's schema or storage poisons the product.

**How to apply:** plugin contract is the small Go interface in
`Specs/FM/orchestrator-plugin-interface.md` (Init, Topics, Subscribe,
Get, List, Health, Close). New orchestrator integration = new adapter
parser + <600-LOC plugin, not an FM core change.

## Protocol 3 — Scope is DV / VM / Container DASH appliance management

**Rule:** Hierarchical object model is HostDeviceObject →
ContainerObject → NICObject. The 5-layer dependency model from Learning
11A applies across all three appliance forms. VM/container-specific
behavior is a NicSpec property, not a separate codepath.

## Protocol 4 — Cardinal rule + sharing matrix are the FM acceptance criteria

**Rule:** Every FM design call is judged by whether it preserves the
**cardinal rule** ("an ENI is programmed only when all dependencies in
Layers 0–3 are resolved AND Layer-4 mapping initial set received") and
respects the **sharing matrix** (per-DPU vs. per-VNET vs. per-ENI scope).

**Why:** These two artifacts from
`Specs/Learning-DashNet/11A-ENI-Dependency-Graph.md` are now load-bearing
as acceptance criteria, not background.

**How to apply:** if a proposal cannot enforce the cardinal rule
mechanically (e.g., via a registry `Ready` gate), reject it. If a
proposal scales work per-ENI for a per-VNET shared object, reject it.

## Protocol 5 — One binary, multiple tiers

**Rule:** The same FM binary runs ultra-small (docker-compose, embedded
etcd) → small → medium → large (TiKV + separate T2 etcd). Only
configuration differs.

**Why:** Customer can evaluate at small scale and grow without
re-platforming. Promotion is `fmctl migrate-data-store` + rolling
restart.

**How to apply:** every new component must work in all four tiers.
Backend choices stay behind narrow interfaces (`DataStore`, `Plugin`,
`Registry[K,V]`). No tier-specific code paths.

## Protocol 6 — Default + every knob a knob

**Rule:** Pick a sensible default for every backend / mode / threshold
choice (etcd as T1; `hash_only` for goal-state durability; rendezvous
hashing for shards). But **every default is a config knob.**

**Why:** Default makes evaluation friction-free; knob keeps the next
customer's needs reachable without a fork.

**How to apply:** ship defaults; document the swap path in the
component's spec. The plugin pattern is the same posture applied to
upstream connectivity.
