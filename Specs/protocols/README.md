# DashFabric Project Protocols

Agent-neutral, repo-checked protocols for working on DashFabric. Point any
AI assistant (Claude, Copilot, Cursor, etc.) at this directory as the
authoritative source of "how we work here."

## How to use

When starting a session with any agent, instruct it to read these files in
order. They are the standing rules for this project — not one-time
decisions. Every spec, design call, and (eventually) implementation choice
is judged against them.

## Protocol files

| # | File | What it covers | Status |
|---|------|----------------|--------|
| 1 | [user-role.md](user-role.md) | Who the architect is, experience level, documentation preferences, working rhythm. | Active |
| 2 | [collaboration-rules.md](collaboration-rules.md) | Debate over reflexive agreement; ask sharpening questions; converge and record after each discussion. | Active |
| 3 | [project-protocols.md](project-protocols.md) | DASH alignment, vendor-neutral FM, scope (DV/VM/Container), cardinal rule, one-binary multi-tier, defaults + knobs. | Active |
| 4 | [implementation-phase.md](implementation-phase.md) | Comprehensive planner, phases/subphases, summary tracker, `next_plan.md` updated each go. | **Inert** until implementation phase begins |
| 5 | [feature-addition.md](feature-addition.md) | Per-feature deliverables: code+tests, e2e verify, `docs/<area>/<feature>.md` with Future Scopes, tracker rows, cross-link. | **Inert** until implementation phase begins |

## Reference layer — where the truth lives

| Concept | Source of truth |
|---------|------------------|
| DASH semantics | [sonic-net/DASH upstream](https://github.com/sonic-net/DASH/tree/main) |
| FM architecture | [../FM/fleet-manager-hld.md](../FM/fleet-manager-hld.md) §3.5–3.6 |
| Dependency model | [../Learning-DashNet/11A-ENI-Dependency-Graph.md](../Learning-DashNet/11A-ENI-Dependency-Graph.md) |
| Storage tiers | [../FM/storage-architecture.md](../FM/storage-architecture.md) |
| Plugin contract | [../FM/orchestrator-plugin-interface.md](../FM/orchestrator-plugin-interface.md) — **superseded by CB** |
| ControllerBridge module | [../CB/](../CB/) — vendor-implemented out-of-process service replacing the in-process plugin |
| CB ↔ FM wire contract | [../cb_fm_protos/](../cb_fm_protos/) — locked gRPC + topic protos |
| Registry pattern | [../FM/registry-pattern-design.md](../FM/registry-pattern-design.md) |
| Recovery & failover | [../FM/recovery-and-failover-design.md](../FM/recovery-and-failover-design.md) |
| Customer tiers | [../FM/deployment-tiers.md](../FM/deployment-tiers.md) |
| Provisioning flow | [../FM/vm-eni-provisioning-design.md](../FM/vm-eni-provisioning-design.md) |
| Project retrospectives | [../me-and-ai/](../me-and-ai/) |

## Activation

Files marked **Inert** above do not apply yet — the project is in spec /
design phase. They activate when the user signals "start implementation"
or the first non-spec artifact (Go file, build config, etc.) is added.

## Updating these protocols

When a discussion converges on a new posture or rule, update the relevant
file here *and* write a retrospective under `Specs/me-and-ai/<topic>.md`
capturing how we got there. Both outputs are required (see
[collaboration-rules.md §Rule 4](collaboration-rules.md)).
