# Review Party implementation plan

Status: active planning ledger

This document orders the work from project inception through replacement of the
current skill-owned execution machinery. It records the shipped local CLI,
remaining acceptance evidence, and future slices so a new session can see the
whole sequence without mistaking the original plan for current state.

The accepted language now lives in [`../CONTEXT.md`](../CONTEXT.md). Provisional
research terms such as `ReviewRequest`, `ReviewPackage`, and `lane` remain
rejected unless they are deliberately reconsidered through domain modeling.

## Status legend

- **Complete**: the slice and its acceptance evidence are present in the
  version-controlled implementation.
- **Implemented; evidence pending**: the code path exists, but one named
  acceptance proof still needs a real external run or environment.
- **Implemented for V1**: the accepted first slice is complete while an
  explicitly recorded extension remains deferred.
- **Implemented locally**: the slice is present and verified in the current
  worktree but has not yet been committed and delivered.
- **Next**: the next slice to execute.
- **Pending**: ordered future work whose details may change based on earlier
  decisions.
- **Decision gate**: work proceeds only if the stated evidence justifies it.

## Project-wide constraints

- Follow [`AGENTS.md`](../AGENTS.md), including path-specific deletion approval,
  dependency approval, focused verification, scratch-space use, and CodeScene
  gates for source files.
- Keep the existing dotfiles `code-review` skill operational until Review Party
  has behavior-based parity for the workflow being moved.
- Separate review intent from harness, agent, model, and transport choices.
- Never report an incomplete external run as clean.
- Never silently weaken or substitute an explicitly selected agent, model,
  transport, profile, or capability guarantee.
- Keep repository delivery governance with the invoking agent and project
  instructions rather than embedding it in the CLI.
- Prefer vertical slices that produce one executable behavior and observable
  evidence. Do not build every abstraction before the first end-to-end run.

## Current implementation baseline — 2026-08-08

Review Party is a working experimental Go CLI with a synchronous, caller-first
`Conductor` Module. It currently provides:

- profile discovery and recipe explanation without Agent Harness launch or a
  Review Record;
- `review-party review [bugs|documentation]` over one frozen working-changes
  Review Subject;
- explicit Grok, OpenCode, or Copilot Reviewer selection with no fallback;
- strict XDG user configuration for the Default Reviewer, Reviewer enablement,
  and per-Reviewer model selection/allowlists;
- direct CLI adapters with repository read/search-only capability backstops;
- finite Attempt deadlines, process-tree cleanup, and bounded diagnostics;
- canonical clean/findings validation with fail-closed Incomplete semantics;
- filesystem-backed Review Records and `review-party inspect`;
- human and JSON output with actual Reviewer provenance; and
- focused tests for subject freezing, lifecycle, adapter decoding, capability
  restrictions, result validation, persistence, and process cleanup.

The current implementation does not provide user-defined Review Profiles,
commit/branch/pull-request Subjects, retry or fallback execution, Verification
Reviews, Parties, ACPX transport, hosted execution, or skill migration.

## Slice 1 — Repository foundation

Status: **Complete**

### Goal

Create a safe repository in which the review tool can be designed and built.

### Completed work

- Initialized `/home/pj/dev/personal/review-party` as a Git repository on
  `main` with an established remote delivery path.
- Adapted the portable agent, deletion, scratch, dependency, CodeScene,
  verification, research, and Go rules from Kashbot into [`AGENTS.md`](../AGENTS.md).
- Added a minimal [`README.md`](../README.md), documentation index, ignored
  `scratch/`, and `docs/research/` convention.

### Acceptance evidence

- Repository exists on `main` with no third-party Go dependencies.
- `scratch/` is ignored by Git.
- Scaffold whitespace checks passed.
- Repository history and remote delivery are established.

## Slice 2 — Architecture and harness research

Status: **Complete**

### Goal

Determine whether Review Party should own review orchestration, ACP transport,
or a complete model/tool harness before implementation begins.

### Completed work

- Researched ACPX versus a direct ACP adapter and the current skill boundary.
- Researched Pi SDK embedding versus established coding-agent harnesses.
- Built a throwaway clickable logic prototype for profile compilation,
  independent reviewer execution, incomplete runs, explicit fallback, and
  remediation verification.

### Durable evidence

- [Pi SDK versus established harnesses](research/pi-sdk-versus-existing-harnesses.md)
- [ACPX and native ACP research](/home/pj/.dotfiles/docs/code-review-cli-acp-research.md)
  in the dotfiles repository
- Logic prototype at
  `/home/pj/.dotfiles/common-agents/.agents/skills/code-review/prototypes/review-engine-logic.PROTOTYPE.html`

### Direction supported by the evidence

- Build Review Party as a capability-aware review orchestrator.
- Initially preserve direct CLI adapters where their native controls matter and
  use ACPX for suitable ACP agents.
- Treat Pi RPC as the first Pi-specific integration if needed.
- Defer Pi SDK embedding and a native Go ACP host until a measured requirement
  cannot be satisfied through existing harnesses and adapters.

This direction remains revisable; it is not a substitute for the domain model.

## Slice 3 — Domain language and review lifecycle

Status: **Complete**

### Goal

Establish the user-recognized concepts, lifecycle, and ownership boundaries for
Review Party before naming types or commands.

### Completed work

- Established the accepted language in [`../CONTEXT.md`](../CONTEXT.md).
- Defined Review, Review Subject, Review Profile, Review Pass, Attempt, Review
  Result, Review Record, Reviewer, Agent Harness, Transport, and provenance.
- Defined Pending, Running, Completed, and Incomplete as the Review Lifecycle.
- Distinguished retry, fallback, and caller-authorized Substitution.
- Defined Verification Review as a new related Review over a changed Subject.
- Kept remediation and delivery authority with the Caller and project rules.
- Modeled Party, Review Pipeline, Review Dependency, and Synthesis Review
  without requiring them in V1.

### Acceptance evidence

- The user can explain each accepted term in concrete review scenarios.
- The lifecycle distinguishes a valid clean outcome from incomplete execution.
- Ownership of policy, execution, and transport behavior is explicit.
- Rejected and deferred concepts are recorded alongside accepted concepts.

### Stop rule

Do not proceed to Slice 4 while core terms still mean different things in
different scenarios.

## Slice 4 — Design the executable contract twice

Status: **Complete**

### Goal

Translate the accepted domain model into the smallest executable CLI boundary
without prematurely copying the current skill implementation.

### Completed work

- Selected the synchronous, caller-first `Conductor` Module documented in
  [`design/conductor-v1.md`](design/conductor-v1.md).
- Kept the external Interface to `Review` and `Inspect` for V1.
- Put Agent Harness variation behind one real internal Attempt-execution seam.
- Kept native event formats, launch flags, and raw diagnostics out of the
  caller-facing Interface.
- Deferred durable `Start`/`Await`/`Cancel`, Party execution, and hosted workers
  until a concrete second lifecycle requires them.

### Acceptance evidence

- The chosen interface is smaller than the behavior it hides.
- Callers do not need to know transport-specific event formats or launch flags.
- Clean, findings, incomplete, cancelled, and invalid-output outcomes cannot be
  confused programmatically.
- The contract supports one end-to-end vertical slice without requiring every
  future adapter or profile.

## Slice 5 — Go bootstrap and one deterministic local path

Status: **Complete**

### Goal

Create the Go module and prove the selected contract without invoking a live
coding agent.

### Completed work

- Initialized the standard-library-only Go module and `review-party` command.
- Implemented the accepted core types and scripted test adapter.
- Compiled the built-in `bugs` Profile Revision and required `bug-review` Pass.
- Added canonical clean/findings parsing and fail-closed incomplete handling.
- Added human and JSON output plus filesystem-backed inspection.

### Verification intent

Before writing tests, use `purposeful-test-design` to identify realistic faults.
At minimum, prove that malformed or incomplete adapter output cannot become a
clean outcome and that an unsupported capability prevents launch.

### Acceptance evidence

- A focused command runs locally without network access or installed agents.
- New analyzable source files satisfy CodeScene and focused Go checks.
- No third-party dependency is added without explicit approval.

## Slice 6 — Immutable review-subject resolution

Status: **Implemented for V1**

### Goal

Resolve the first agreed user-facing Git target into an immutable, inspectable
subject for repeatable review.

### Completed work

- Implemented the working-changes Review Subject as the only V1 target form.
- Captured tracked and untracked changes, changed paths, absolute repository
  root, binary diff payload, and a stable content identity.
- Rejected an empty Subject and froze its payload before Attempt execution.
- Kept every Git invocation argv-based and repository-scoped.

### Deferred extension

- Add an explicit oversized/materiality outcome before introducing larger
  Subject forms. Current capture is bounded later at harness output, not at
  Review Subject construction.

### Acceptance evidence

- The same resolved subject yields the same identity and payload.
- Untracked files follow an explicit, tested rule.
- Empty Subjects are rejected, and later working-tree mutation cannot alter the
  frozen Subject used by a Running Review.
- Oversized Subject handling remains an explicit follow-up rather than silent
  truncation.

## Slice 7 — First live adapter and end-to-end review

Status: **Complete**

### Goal

Run one real review through the best-supported initial harness while preserving
the selected domain and output contracts.

### Completed work

- Implemented direct Grok, OpenCode, and Copilot adapters behind the same
  Attempt-execution seam.
- Added explicit argv, working directory, model/effort selection, read/search
  capability restrictions, non-interactive execution, deadlines,
  cancellation, bounded capture, and process-tree cleanup.
- Normalized native event streams without exposing their schemas to the core
  domain types.
- Centralized canonical result validation and honest Attempt Outcomes.
- Added fixture-backed adapter decoder, failure, overflow, and cleanup tests.

### Dated live evidence

- Copilot completed a bounded Documentation Review after a decoder correction
  for native assistant-message boundaries; it remains secondary compatibility
  coverage rather than the primary verifier.
- OpenCode Muse and Grok both completed bounded Bug Reviews of Review Party's
  own non-empty Slice 8 changes and produced actionable findings that were
  assessed and remediated.
- OpenCode DeepSeek reached the explicitly selected model but hit its
  five-minute deadline, so that model-specific run remains honestly Incomplete.
- Commands, immutable Subject identities, Review IDs, outcomes, and finding
  dispositions are retained in
  [`research/live-adapter-audit-2026-08-09.md`](research/live-adapter-audit-2026-08-09.md).

### Acceptance evidence

- A real bounded review completes on an immutable subject.
- Missing binary, rejected model, timeout, denied tool, malformed output, and
  non-zero exit paths remain incomplete rather than clean.
- Fixture replay covers the adapter decoder without requiring live model calls.

## Slice 8 — Named review profiles

Status: **Implemented locally**

### Goal

Expose the first user-recognized specialized review behaviors without
conflating intent with runtime details.

### Completed work

- Added the domain-backed `documentation` Profile beside `bugs`.
- Gave each Profile an explicit purpose, materiality threshold, required Pass,
  prompt revision, capability contract, Attempt Limit, Execution Deadline, and
  canonical result-contract revision.
- Added deterministic `profiles` discovery and subject-independent `explain`
  behavior in human and JSON formats.
- Kept Reviewer/model/Agent Harness/Transport defaults visible in the effective
  Profile Revision.
- Added typed unknown Profile, unknown Reviewer, and capability-mismatch errors.
- Added caller-owned, versioned user configuration so Reviewer enablement and
  model preferences are resolved before Subject resolution and participate in
  the effective Profile Revision.
- Removed the personal OpenCode model choice from the in-process catalog;
  OpenCode now requires a configured or explicit allowed model.
- Rejects a capability mismatch before Review Subject resolution, Availability
  Check, Review Record creation, or Agent Harness launch.
- Kept definitions built in; no inheritance, arbitrary scripting, Parties,
  fallback, or new profile-provider seam was added.

### Acceptance evidence

- Two accepted profiles produce meaningfully different, explainable review
  behavior.
- A profile fails before launch when its adapter cannot meet its requirements.
- Profile selection does not alter project governance or editing authority.

## Slice 9 — ACPX transport adapter

Status: **Pending**

### Goal

Add an ACP-backed harness without making ACPX semantics part of the Review Party
domain model.

### Operations

1. Wrap ACPX one-shot execution behind the same capability-aware adapter
   boundary.
2. Support the agreed read/search-only contract, no-terminal behavior,
   cooperative timeout, hard watchdog, cancellation, and structured events.
3. Normalize ACP events, permissions, stop reasons, usage, and diagnostics.
4. Pin or verify compatible ACPX behavior at launch because ACPX is pre-1.0.
5. Compare the result contract with the direct adapter using shared fixtures.

### Acceptance evidence

- The core engine cannot tell whether a successful result came from direct CLI
  or ACPX except through recorded adapter metadata.
- ACP permission mediation is not mislabeled as OS sandboxing.
- Protocol or adapter incompatibility produces a precise incomplete result.

## Slice 10 — OpenCode and Pi adapter experiments

Status: **Decision gate**

Direct OpenCode execution now exists. The native-versus-ACPX comparison and all
Pi experiments remain gated on a named Profile demonstrating a concrete need.

### Goal

Measure which native or ACP surface earns durable adapter support rather than
choosing one universal harness in advance.

### Operations

1. Use the same immutable subject, accepted profile, model where possible,
   capability contract, and result schema.
2. Compare native OpenCode with ACPX-to-OpenCode.
3. Compare Pi RPC with ACPX-to-Pi only if a desired profile benefits from Pi.
4. Record launch-to-first-event, wall time, selected model, visible tools,
   attempted and denied calls, stop reason, incomplete signals, token/cost data,
   event fidelity, schema failures, and child-process cleanup.
5. Promote only integrations that offer a named profile a concrete advantage.

### Stop rule

Do not embed Pi SDK or build a native Go ACP host merely to remove a subprocess.
Open a dedicated design slice only when the research note's concrete trigger
conditions are demonstrated by measurements.

## Slice 11 — Fix verification and bounded continuation

Status: **Pending**

### Goal

Support the accepted relationship between an initial review and checking fixes
without restarting an expensive broad audit.

### Operations

1. Accept the prior validated outcome, local dispositions, and exact remediation
   delta using the domain relationship chosen in Slice 3.
2. Keep verification scoped to prior accepted findings and regressions caused by
   their fixes.
3. Preserve reviewer identity when the domain model requires continuity.
4. Enforce a finite pass budget and a single diagnosed transport retry rule.
5. Surface residual risk rather than chasing a clean result indefinitely.

### Acceptance evidence

- A scoped fix check consumes only the evidence needed for prior findings.
- Resolved findings are not reopened by unchanged-code exploration.
- The workflow cannot automatically enter an unbounded review/fix loop.

## Slice 12 — Thin skill integration and migration

Status: **Pending**

### Goal

Move deterministic execution out of the dotfiles skill while retaining its
human-facing policy, validation, and governance responsibilities.

### Operations

1. Add a Review Party invocation path to the existing skill without deleting
   the current runner.
2. Compare behavior on a fixed corpus of clean, findings, incomplete, malformed,
   unavailable-agent, timeout, and fix-verification scenarios.
3. Keep local finding validation, authorization-sensitive remediation, project
   governance, and presentation in the skill.
4. Switch the skill default only after parity evidence is reviewed.
5. Request path-specific approval before deleting or archiving replaced skill
   files.

### Acceptance evidence

- The skill becomes a short invocation and governance workflow rather than an
  executable specification.
- Existing supported review behavior has explicit parity evidence.
- Rollback to the prior runner remains possible during migration.

## Slice 13 — Delivery baseline

Status: **Pending**

### Goal

Establish the first supported local release and close the initial migration.

### Operations

1. Audit every accepted domain invariant and slice acceptance criterion.
2. Run focused verification, final CodeScene safeguards, and the repository's
   eventual CI gate.
3. Document installation, required external harnesses, profile inspection,
   diagnostics, and incomplete-run semantics.
4. Create the requested commits, remote, and release artifacts only with the
   user's delivery authorization.
5. Record deferred adapters and harness-ownership triggers as future work rather
   than implicit scope.

### Acceptance evidence

- A fresh local installation can run the supported profiles and diagnose missing
  external requirements.
- Documentation and observed CLI behavior agree.
- The old skill execution path is retired only with explicit deletion approval
  and recovery evidence.

## Immediate next action

Close the local Slice 8 change set:

1. Run focused Go, race, vet, build, and CodeScene verification.
2. Review the retained Grok/OpenCode dogfood findings and their regression
   evidence.
3. Commit and deliver Slice 8 only with explicit delivery authorization.

After delivery, evaluate Slice 9 against the existing ACPX research before
starting implementation. Do not start Verification Review, Party execution, or
skill migration as part of Slice 8 closeout.
