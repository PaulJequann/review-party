# V1 conductor interface

Status: accepted for implementation

## Decision

V1 uses a synchronous, caller-first `Conductor` Module. Its external Interface
contains only the behavior the first vertical slice implements:

```go
record, err := conductor.Review(ctx, selection)
record, err := conductor.Inspect(ctx, reviewID)
```

The CLI presents the common path as:

```text
review-party run
review-party inspect <review-id>
```

`Run` replaced the earlier `review` and `party run` commands and executes the
repository's saved selection or one explicit Profile or Party. Durable
`Start`/`Await`/`Cancel` lifecycle control is deferred until hosted execution
provides a concrete need. A host can wrap the synchronous operation in a job
runner without exposing Review Party's internal execution machinery.

## Deep module seam

The Conductor hides:

- mutable-reference resolution into one fixed Review Subject;
- Profile Revision resolution and prompt construction;
- Context Discovery instructions;
- capability and Availability Checks;
- Attempt supervision, deadline, and process-tree cleanup;
- output decoding and Review Result validation;
- honest Completed versus Incomplete semantics; and
- durable Review Record creation and inspection.

The external Agent Harness is reached through one internal Attempt-execution
seam. V1 has direct Grok, OpenCode, Copilot, and Codex adapters plus a scripted
test adapter, so the seam represents real variation rather than speculative
indirection.

## First dogfooded slice

- Working-changes and committed-range Review Subjects, frozen before execution.
- Committed ranges resolve full base/head commit IDs and run repository access
  in a detached, Review Party-owned worktree at the recorded head. One Subject
  execution Interface hides preparation, ownership validation, cleanup after
  process exit, and bounded inactive-leftover reconciliation.
- Packaged `bugs`, `code-quality`, and `documentation` Templates plus complete
  repository/global executable Profiles resolved through the Configuration
  Manager, each compiling to one required Review Pass.
- Saved Profiles select one exact Grok, OpenCode, Copilot, or Codex Reviewer
  candidate; ordinary Reviews do not override that choice.
- Ordinary Reviews use Attempt Limit one and the saved finite deadline. Eval
  experiments may use bounded retries; fallback remains deferred.
- Native adapter-specific read/search-only tool availability and permission
  backstops.
- Canonical clean/findings result validation.
- Honest Incomplete Review for an unavailable Reviewer, authentication failure,
  deadline, cancellation, transport failure, or malformed output.
- Filesystem-backed Review Records and `inspect`.
- Human output plus JSON output for agent and automation callers.
- A real Review of Review Party's own implementation changes through the CLI.

## Later additions and remaining work

Flat Party execution, bounded concurrency, and finite retries were added after
the first slice. Review Dependencies, fallback, remote pull-request resolution,
Verification Review, Synthesis Review, hosted workers, and native ACP hosting
remain deferred. Packaged Documentation and Code Quality material is now
non-executable Template content.

Current update: bounded concurrency, finite retries, and flat scoped Party
execution with persisted Review Bundles now exist on top of this Conductor. The
accepted Party contract is in [`party-v1.md`](party-v1.md). Review Dependencies,
Synthesis Review, conditional pipelines, and hosted execution remain deferred.

## Acceptance evidence

- Later working-tree changes cannot alter the fixed Subject recorded for a
  Running Review.
- Only a structurally valid clean or findings result can complete the Review.
- Missing or malformed reviewer output is Incomplete, never clean.
- A failed Availability Check launches no Agent Harness and consumes no Attempt.
- A stuck Attempt ends at its finite deadline.
- Completed and Incomplete Review Records remain retrievable.
- Every direct adapter sees only repository read/search tools, with mutation,
  shell, web, and undeclared extensions denied as backstops.
