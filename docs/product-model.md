# Review Party product model

Status: accepted domain model; implementation design pending

## Product purpose

Review Party conducts bounded, capability-aware code reviews through external
coding agents. It turns a caller's chosen subject and review profile into a
retrievable, provenance-rich outcome without deciding whether changes may ship.

V1 targets the review-conductor role now spread across the existing code-review
skill. Future versions may add project history, reusable review pipelines,
peer-review tiers, synthesis, and richer assessments without absorbing delivery
authority.

## Core relationships

```text
Caller
├── selects one Profile ──> Review ──> Review Result + Review Record
└── selects a Party ──────> Review Pipeline ──> Review Bundle
```

A Review evaluates one fixed Review Subject with one exact Profile Revision.
It may contain multiple independent Review Passes. A Party applies several
Profiles to one shared Subject; the effective Party composition is frozen
before execution.

Different Profiles or changed Subjects remain separate, related Reviews. A
Verification Review checks changed code against an earlier Review Result. A
Synthesis Review may consume selected upstream Results without replacing their
records or provenance.

The canonical language and distinctions are defined in [`../CONTEXT.md`](../CONTEXT.md).

## Responsibility map

### Caller

- Identifies what should be reviewed and selects a Profile or Party.
- Supplies explicit authority for a Substitution when required.
- Consumes the Review Record or Review Bundle.
- Decides whether to remediate, defer, accept risk, or deliver.

### Review Party

- Resolves a mutable reference into one fixed Review Subject.
- Resolves and freezes effective Profile and Party Revisions.
- Plans Passes and explicit Review Dependencies.
- Selects only declared Reviewer candidates and verifies required capabilities.
- Bounds concurrency, Attempts, retries, fallback, deadlines, and cleanup.
- Normalizes Attempt Outcomes and validates result structure.
- Preserves provenance, diagnostics, completeness, and partial evidence.

### Reviewer and Agent Harness

- The Reviewer supplies non-deterministic investigation and judgment.
- The Agent Harness owns its model/tool loop, context assembly, and tool
  execution.
- The Transport carries execution between Review Party and the harness without
  defining the Reviewer or review purpose.

### Project governance

- Explicitly applicable Project Rules constrain the review.
- The project and Caller determine the consequence of Findings and Assessments.
- Project documentation does not become authoritative merely because it exists.

## Guarantees and judgment

Review Party can guarantee the exact Subject, effective revisions, actual
Reviewer/harness/transport provenance, bounded execution, valid result shape,
and honest completion state. It cannot guarantee that a model finds every
issue or that every Finding is factually correct.

A Finding is an evidence-bearing reviewer claim. Absence of evidence must not
be promoted into evidence of absence. For example, when a Project Rule requires
approval for new dependencies and a Subject adds one, a reviewer may report
that approval is not established by the available evidence; it must not claim
that approval was denied or never granted.

## Project material

Context Discovery begins leanly:

- Inspect applicable `AGENTS.md` instructions as Project Rules.
- Look for `CONTEXT.md` or `CONTEXT-MAP.md` and use the applicable domain
  language.
- Consult repository orientation such as README files when relevant.
- Pursue ADRs, plans, research, and other documentation only when the Subject or
  emerging evidence warrants it.

Domain context, ADRs, plans, README files, comments, and existing code may all
be stale or contradictory. A material conflict is surfaced with provenance and
uncertainty rather than adjudicated from file type or location.

## Completion semantics

A Review is Pending after its Subject and Profile Revision are fixed, Running
while promised work remains, and then Completed or Incomplete. Internal adapter
phases and termination causes do not become additional Review states.

Completion and conclusion are independent. An Incomplete Review or Bundle may
still contain actionable Findings from completed work. It may never turn
partial coverage into a clean conclusion.

Every selected Review in a Party is required unless the effective Party
Revision explicitly says otherwise. An incomplete required Review produces an
Incomplete Bundle while preserving all completed Results.

## Recovery and concurrency

Each Pass has one finite Attempt Limit shared by every Reviewer candidate. Each
executed harness launch consumes an Attempt; an Availability Check rejected
before launch does not. Every Attempt and Party also has a finite deadline.

Recovery distinguishes:

- retry: another Attempt using the same Reviewer after an eligible failure;
- fallback: advancing to the next candidate in the declared Fallback Chain;
- substitution: using an undeclared agent, model, transport, profile, or weaker
  capability contract, which requires Caller authority.

Profiles and Parties provide opinionated Recovery Policy defaults. Advanced
Caller overrides produce recorded effective revisions. Reviewer Preference may
reorder declared candidates; Reviewer Requirement narrows them.

Independent ready work may execute concurrently up to a finite Party limit.
Review Dependencies impose ordering, and all Attempts for one Pass remain
sequential. Fallback is recovery, not an extra peer opinion.

## Configuration and observability

Routine callers select a Profile, a named Party, or an ad hoc Profile
composition. They do not need to configure every model, transport, retry,
fallback, or timeout.

Every effective Profile or Party change is retained as a distinct revision so
prompt, model, reasoning, harness, recovery, and composition experiments can be
compared. This provides reproducibility of inputs and execution provenance, not
deterministic model output.

Every Review creates a retrievable Review Record. Normal output favors the
concise Review Result; deeper inspection exposes Passes, Attempts, effective
configuration, diagnostics, and available logs. A Review Bundle aggregates
records without erasing their individual provenance or completeness.

## Initial product direction

- Keep Review Party a review conductor rather than an embedded agent harness.
- Preserve useful native harness controls behind capability-aware adapters.
- Keep agent identity and transport identity separate.
- Fail closed on unavailable required capabilities, invalid results, unknown
  failures, undeclared fallback, and exhausted recovery budgets.
- Make the first implementation capable of reviewing Review Party's own
  changes through the same public interface used by other callers.

Package structure, command vocabulary, storage format, configuration format,
and the exact first implementation slice remain interface-design decisions.
