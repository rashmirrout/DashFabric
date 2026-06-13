# Implementation Phase Protocol

When the project transitions from spec phase to implementation, this
protocol governs how work is structured. **Not yet active — the project
is still in spec phase.** Activates when the user signals "start
implementation" or works on the first executable artifact.

## Rule 1 — Comprehensive planner at the very beginning

**Rule:** Before writing any production code, produce a *very, very
comprehensive* implementation planner. Not a sketch — the full plan.

**Why:** *"create a very very comprehensive planner at the beginning of
implementation."* Without it, implementation drifts from the design
specs and the cardinal rule erodes silently.

**How to apply:** the planner lives at `Specs/implementation/planner.md`
(or equivalent). It enumerates every component to build, its dependency
on prior components, its acceptance criteria (tied back to spec
artifacts), and its phase/subphase. It is reviewed with the user before
any phase 1 code lands.

## Rule 2 — Divide into phases and subphases

**Rule:** Implementation is organized as **Phase N → Subphase N.M**.
Each subphase has a clear deliverable, exit criteria, and dependencies.

**Why:** *"divide into phases and subphases."* Big-bang delivery hides
regressions; phased delivery surfaces them early.

**How to apply:** typical phase taxonomy:

- Phase 0 — scaffolding (repo layout, build, CI, test harness).
- Phase 1 — T1/T2/T3 storage interfaces with an in-memory backend.
- Phase 2 — registry skeletons (Acquire/Release/Read).
- Phase 3 — plugin interface + one reference plugin.
- Phase 4 — adapter (ingest path).
- Phase 5 — NicActor + compose path.
- Phase 6 — slim HDO + HAL.
- Phase 7 — recovery flows.
- Phase 8 — tier ladders (compose, K8s small, etc.).

Each subphase has its own subsection in the planner.

## Rule 3 — Summary tracker, updated each go

**Rule:** Maintain a `Specs/implementation/tracker.md` (or similar) that
summarizes progress at a glance — phases, subphases, status, artifacts
produced, links to design docs.

**Why:** *"create summary tracker. update the status at each go."*
Without it, "where are we?" requires re-reading the planner each time.

**How to apply:** tracker has columns: phase, subphase, status (todo /
in-progress / done / blocked), artifacts (PRs, doc paths), notes.
Updated *every* working session, not at phase boundaries.

## Rule 4 — `next_plan.md` updated at each step

**Rule:** Maintain a `next_plan.md` (location TBD — likely
`Specs/implementation/next_plan.md`) that captures the *immediate next
step* and any blockers. Updated at the end of every working go.

**Why:** *"each go shall update a next_plan.md."* Resumption from a
break is then instant — read `next_plan.md`, continue.

**How to apply:** at end of session, before exiting, update
`next_plan.md` with: (a) what was just done, (b) what's the very next
action, (c) open questions / blockers, (d) any context the next session
needs to pick up from cold.

## Activation signal

This protocol activates when:

- The user explicitly says "start implementation" / "let's code" / etc.
- The first non-spec artifact (Go file, build config, etc.) is added.
- The user asks for a planner, tracker, or `next_plan.md`.

Until then, the project stays in spec/design phase and this protocol is
inert.
