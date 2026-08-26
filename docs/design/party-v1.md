# Party composition and Review Bundles v1

Status: implemented behavior; superseded for future work by Configuration Hub Slice 4

> **Accepted replacement, 2026-08-24:** Parties become flat ordered groups of
> scoped Review Profile references. The replacement removes `extends`, nested
> Party composition, inherited concurrency, and member Reviewer/model/effort
> pins. Repository Configuration performs the roll-up by selecting Global and
> Repository Profiles or Parties. See
> [`../configuration-hub-implementation-plan.md`](../configuration-hub-implementation-plan.md).
> This document remains an accurate record of the currently shipped PR #9
> behavior until the replacement slice lands.

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
- Party names, `defaults.party`, and every `extends` entry must match
  `[a-z0-9][a-z0-9-]*`; the `name` field must equal the file name. Unknown
  fields, unsupported schema versions, empty member lists (including on an
  extending definition), duplicate Profile or parent entries, self-extension,
  and invalid concurrency limits fail before anything launches.
  `concurrency_limit` must be omitted or a non-negative integer; explicit
  `null` and negative values are invalid. Zero behaves like omission for
  inheritance and the sequential fallback. Cycles and unknown parents also
  fail before launch.
- Members may pin reviewer/model/effort. Explicit caller flags narrow every
  member to one choice and never substitute a member's declared selection.

## Layered composition

A Party may declare `extends`: an ordered list of other party names resolved
through normal Repository, Personal, then packaged precedence. This is how a
Personal baseline composes with repository-specific steps in one seamless run:

```json
{
  "schema_version": 1,
  "name": "release-gate",
  "extends": ["org-baseline"],
  "profiles": [{"profile": "code-quality", "reviewer": "codex", "effort": "high"}]
}
```

- Inherited members run first in their declared order; genuinely new local
  members append after them.
- Across parents, a later parent in `extends` order replaces an earlier
  parent's member settings at that Profile's original position. A local member
  then replaces inherited settings at the same position. These are
  definition-time composition rules, not runtime Substitution.
- A Party's own `concurrency_limit` wins when set. Otherwise, the first parent
  in `extends` order that provides a nonzero limit supplies the inherited bound.
- Cycles and unknown parent names fail closed before any launch. A parent name
  resolves through the same shadowing rules as a direct request.
- The composition produces ONE Review Bundle: all members review the single
  frozen Subject through the ordinary path. Personal baselines and repository
  additions are never split across runs.

`review-party parties` shows each valid definition's declared `extends` chain
and local `profiles`, not the flattened effective members; the persisted Bundle
records the effective members after `party run`. An invalid definition reports
its decoding or validation error instead. Bare
`review-party party run` resolves `defaults.party` from Repository then
Personal Configuration, falling back to packaged `standard`. A positional Party
name is explicit and takes precedence over every configured default.

## Execution contract

- Preflight resolves the repository root and Subject exactly once, compiles
  every member Profile Revision against the catalog, and only then creates the
  Bundle row. Any compilation failure aborts the Party with zero attempts and
  no Bundle.
- The Party Revision digest freezes the flattened effective composition: name,
  concurrency limit, each member's effective reviewer/model/effort, and each
  compiled Profile Revision hash. Different authored `extends` chains that
  flatten to the same effective composition share a revision. Flag overrides
  produce a distinct revision rather than silently overwriting effective state.
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
review-party party run [PARTY] [--reviewer ID] [--model MODEL] [--effort EFFORT]
    [--concurrency N] [--repo PATH] [--base COMMIT --head COMMIT]
    [--deadline DURATION] [--config PATH] [--format human|json]
review-party inspect rb_... [--format human|json]
```

`party run` exits 2 when the resulting Bundle is Incomplete, mirroring single
Reviews.

## Non-goals for this slice

Review Dependencies, Synthesis Review, conditional or ordered pipelines,
per-member retry policies, optional (non-required) members, Party-level result
merging, ad hoc multi-party composition on one command line, and hosted
execution remain deferred. The Bundle aggregates Records; it never merges
Findings into a single verdict and never acquires delivery authority.
