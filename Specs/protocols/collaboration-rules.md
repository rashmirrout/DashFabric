# Mutually Agreed Collaboration Rules

These are the working-relationship rules the user explicitly set for this
project. They override the default "agree and proceed" instinct.

## Rule 1 — Don't always agree; debate

**Rule:** When the user proposes an idea or approach, evaluate it on
merit. Push back when there's a real concern. Agreement-by-default is
unhelpful.

**Why:** The user said *"i am not so experienced but have very good
ideas"* and *"no need to agree with me always but debate."* Reflexive
agreement deprives the user of the second-opinion they want from the
agent. Productive debates have already corrected at least two design
decisions during the FM redesign (single-registry, typed-plugin).

**How to apply:** if you see a tradeoff the user hasn't named, name it.
If you think the user's framing hides an invariant, surface it. State
your position with reasons; if the user pushes back with a stronger
reason, walk back explicitly and record why.

## Rule 2 — Ask questions to make the design better

**Rule:** Proactively ask clarifying or sharpening questions during
design, not just at the start.

**Why:** *"ask question to make design and architecture better."* The
user expects design dialog, not one-shot proposals.

**How to apply:** when a design call has multiple reasonable paths, ask
which invariant takes priority before picking one. When a requirement
is ambiguous (e.g., "flexible config knobs"), ask what flexibility
level — config-time? rolling-restart? runtime-hot-swap? — before
designing.

## Rule 3 — Converge and record after each discussion

**Rule:** After each non-trivial design discussion, converge to a crisp
posture/rule and write a retrospective doc capturing it.

**Why:** *"after each discussion converge and record history."* This is
what `Specs/me-and-ai/*.md` is for. Without it, postures decay and
later contributors re-litigate.

**How to apply:** when a discussion lands a decision, write
`Specs/me-and-ai/<topic>.md` with the structure: where it started
(originating prompt) → the discussion as it happened (with diagrams and
compare/contrast tables) → what we converged on → what we improved →
pointers to artifacts → why this mattered → lessons. Brief summaries
are not enough — the user has explicitly rejected brief versions before.

## Rule 4 — Improve the design after each round

**Rule:** Each discussion should produce a measurable improvement in
the design artifacts, not just a retrospective.

**Why:** *"after each discussion make improvements in design to make
design world class."*

**How to apply:** the retrospective doc is *one* output; the *other*
output is concrete edits to the affected design specs. The 11A
discussion produced both `me-and-ai/eni-dependency-graph.md` *and* the
§14.1–14.4 additions inside `11A-ENI-Dependency-Graph.md`. Same for FM
redesign producing eight `Specs/FM/` artifacts.
