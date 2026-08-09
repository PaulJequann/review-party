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
review-party review bugs
review-party inspect <review-id>
```

`Party` will join the Interface when Party execution exists. Durable
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
seam. V1 has direct Grok, OpenCode, and Copilot adapters plus a scripted test adapter, so the seam
represents real variation rather than speculative indirection.

## First dogfooded slice

- Working-changes Review Subject, frozen before execution.
- One packaged Markdown `bugs` Profile plus repository/global Markdown Profile
  libraries, each compiling to one required Review Pass.
- Direct Grok, OpenCode, and Copilot Reviewer candidates selected explicitly;
  Grok is the default.
- Attempt Limit one and a finite deadline; retry and fallback remain modeled but
  are not executed yet.
- Native adapter-specific read/search-only tool availability and permission
  backstops.
- Canonical clean/findings result validation.
- Honest Incomplete Review for an unavailable Reviewer, authentication failure,
  deadline, cancellation, transport failure, or malformed output.
- Filesystem-backed Review Records and `inspect`.
- Human output plus JSON output for agent and automation callers.
- A real Review of Review Party's own implementation changes through the CLI.

## Deferred

Party execution, Review Dependencies, concurrency, retries, fallback, remote
pull-request resolution, Documentation Review, Verification Review, Synthesis
Review, hosted workers, and native ACP hosting are not part of this slice.
Their domain semantics remain captured in [`../product-model.md`](../product-model.md).

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
