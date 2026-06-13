# Feature Addition Deliverable Protocol

For every feature/slice landed during implementation, this is the
**non-negotiable** deliverable list. **Not yet active — implementation
hasn't started.** Activates once the implementation phase begins (see
[implementation-phase.md](implementation-phase.md)).

## Per-feature deliverables (every feature, every time)

| # | Deliverable | Notes |
|---|-------------|-------|
| 1 | **Code + tests** | Production code with unit tests covering golden + edge cases. |
| 2 | **Live e2e verification** | Feature exercised in a real running stack (compose or K8s), not just unit-tested. Document the verification steps. |
| 3 | **`docs/<area>/<feature>.md`** | Feature doc with: problem, solution, wire/config, implementation, tests, **Future Scopes** section. |
| 4 | **Tracker rows updated** | The `tracker.md` row for this feature is updated with a link to the feature doc. |
| 5 | **Cross-link** | The area's `features.md` / overview is updated to reference the new feature doc. |

## Feature doc template

`docs/<area>/<feature>.md` must include these sections:

1. **Problem** — what gap or requirement this feature addresses.
2. **Solution** — the design choice taken; cross-link to relevant spec.
3. **Wire / Config** — protobufs, REST endpoints, config knobs added.
4. **Implementation** — file/module layout, key types, decision points.
5. **Tests** — unit tests, integration tests, e2e verification steps.
6. **Future Scopes** — what's deferred, why, and what would unlock it.

## The "no doc-per-typo bloat" exception

**Rule:** If a slice doesn't earn its own doc (e.g., a 1-line bug fix, a
typo, a trivial config tweak), it goes into the **parent feature's doc
as a Change Log entry** instead. Same audit trail, no doc-per-typo bloat.

**How to apply:** judgment call — does the change need its own
problem/solution/test narrative? If yes, new doc. If it's a small
follow-up to an existing feature, append to that feature's doc under a
`## Change Log` section with date + brief description.

## Why this protocol exists

Without per-feature docs:

- Future contributors re-derive the rationale from `git log`, which rots.
- Tests-as-documentation is insufficient for *why* the design is shaped
  this way.
- The "Future Scopes" section is what saves us from re-litigating
  decisions in the next round.

With per-feature docs + tracker rows + cross-links, every feature has a
permanent home where its problem, solution, tests, and deferred work all
live together.

## Activation signal

Activates when implementation phase activates (see
[implementation-phase.md](implementation-phase.md)). Until then this
protocol is inert.
