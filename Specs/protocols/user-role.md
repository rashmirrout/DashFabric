# User Role & Project Context

The user is the **architect** of **DashFabric**, a production-grade
distributed network control plane for DPU-enabled datacenters.

## Self-described experience level

- **"I am not so experienced but have very good ideas."** Treat the user
  as the *idea source* and decision authority, but do not assume deep
  prior experience with every distributed-systems primitive,
  K8s/etcd/RocksDB internal, DASH SAI table semantics, etc.
- **How to apply:** when introducing a non-trivial concept, give the
  short framing first (1–2 sentences) before diving into the design call.
  Don't gate the design on assumed prior knowledge — surface it.
- Familiar with the **DASH networking stack** at the architecture level;
  comfortable reading SONiC/DASH docs but expects DashFabric docs to make
  lineage explicit (the upstream-DASH alignment posture).

## Role & authority

- **Architect** of DashFabric. Final decision-maker on scope, invariants,
  and posture (e.g., "default etcd, every knob a knob"; "callout, don't
  rename"; "cardinal rule").
- **Not** a hands-on implementer in this session — the work is design
  artifacts (specs, retrospectives), not code yet.

## Documentation preferences

- **Comprehensive over brief.** Multiple times the user has pushed back
  when docs were too short, missing diagrams, or missing
  compare-and-contrast tables.
- **Diagrams when they add clarity.** Mermaid for sequence, flowchart,
  state machine, dependency DAG, layered architecture. Not everywhere —
  only where prose alone hides ordering or sharing.
- **Compare-and-contrast tables** are first-class. Before/after,
  alternatives-considered, sharing-scope, failure-mode-per-layer.
- **Worked examples** preferred over abstract description (e.g.,
  cold-path vs. fast-path ENI provisioning).
- **Pointers to artifacts** at the end of every retrospective.

## Working method (the rhythm we've established)

1. User states intent or problem.
2. Agent proposes; user pushes back or accepts.
3. **Both pushback and acceptance get recorded** (see
   [collaboration-rules.md](collaboration-rules.md)).
4. Converge to a posture/rule, not just a one-time edit.
5. After each major discussion, write a `Specs/me-and-ai/<topic>.md`
   retrospective with: where it started, the discussion as it happened
   (with diagrams), what we converged on, what we improved, pointers to
   artifacts.
