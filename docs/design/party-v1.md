# Party composition and Review Bundles v1

Status: implemented locally; live dogfood evidence in the implementation plan

A Party is a Caller-selected composition of Review Profiles applied to one
Review Subject. Its effective composition is fixed before it becomes a Review
Pipeline, and its aggregate output is one persisted Review Bundle that preserves
each constituent Review Record without erasing individual provenance or
completeness.

## Definition surface

Reusable Parties are strict version-1 JSON documents:

```json
{
  "schema_version": 1,
  "name": "release-gate",
  "description": "Pre-delivery sweep",
  "concurrency_limit": 2,
  "profiles": [
    {"profile": "bugs"},
    {"profile": "code-quality", "reviewer": "codex", "model": "m", "effort": "high"}
  ]
}
```

- Repository layer: `.reviewparty/parties/<name>.json`; Personal layer:
  `${XDG_CONFIG_HOME:-$HOME/.config}/review-party/parties/<name>.json`.
  Repository shadows Personal; packaged definitions are the last fallback.
  Shadowing replaces the whole definition.
- The `name` field must equal the file name. Unknown fields, unsupported
  schema versions, empty member lists, duplicate profile entries, and negative
  concurrency limits fail validation before anything launches.
- Members may pin reviewer/model/effort. Explicit caller flags narrow every
  member to one choice and never substitute a member's declared selection.

## Execution contract

- Preflight resolves the repository root and Subject exactly once, compiles
  every member Profile Revision against the catalog, and only then creates the
  Bundle row. Any compilation failure aborts the Party with zero attempts and
  no Bundle.
- The Party Revision digest freezes the effective composition: name,
  concurrency limit, each member's effective reviewer/model/effort, and each
  compiled Profile Revision hash. Flag overrides therefore produce a distinct
  revision rather than silently overwriting recorded provenance.
- Members execute through the same ordinary `Review` path as standalone
  Reviews, including capability backstops, canonical result validation,
  fail-closed Incomplete semantics, artifacts, and per-member Attempt records.
- Concurrency bounds active Reviewer executions at the effective limit
  (default one). A limit of N starts all members immediately but gates harness
  execution through the same bounded gate used by Eval Suites. Retry backoff,
  cancellation, and process cleanup keep their existing semantics.
- Each terminal member is checkpointed onto the Bundle in manifest order
  regardless of completion order.

## Completion semantics

- Completed: every required member Review completed.
- Incomplete (coverage): at least one member ended Incomplete while the full
  manifest was attempted. No termination fact is recorded on the Bundle;
  each affected child carries its own typed termination.
- Incomplete (hard stop): cancellation or a persistence failure stops planned
  work. Already-launched children are drained so their persisted Reviews stay
  linked instead of being orphaned as pending members, and the Bundle records
  a categorical termination with its cause.

An Incomplete Bundle never disguises missing coverage as clean, and completed
members remain visible with their findings.

## CLI

```text
review-party parties [--repo PATH] [--format human|json]
review-party party run PARTY [--reviewer ID] [--model MODEL] [--effort EFFORT]
    [--concurrency N] [--repo PATH] [--base COMMIT --head COMMIT]
    [--deadline DURATION] [--config PATH] [--format human|json]
review-party inspect rb_... [--format human|json]
```

`party run` exits 2 when the resulting Bundle is Incomplete, mirroring single
Reviews.

## Non-goals for this slice

Review Dependencies, Synthesis Review, conditional or ordered pipelines,
per-member retry policies, optional (non-required) members, Party-level result
merging, and hosted execution remain deferred. The Bundle aggregates Records;
it never merges Findings into a single verdict and never acquires delivery
authority.
