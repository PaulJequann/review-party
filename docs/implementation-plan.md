# Review Party implementation plan
Status: active planning ledger
Last reconciled: 2026-08-19

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
- **In progress**: one slice is the active implementation scope; its acceptance
  checklist is not yet complete.
- **Next**: the next slice to execute.
- **Pending**: ordered future work whose details may change based on earlier
  decisions.
- **Decision gate**: work proceeds only if the stated evidence justifies it.

## Execution cursor

This checklist is the authoritative current position. A fresh agent should start
with the first unchecked item, verify its prerequisites, and update both the
slice status and this checklist when the acceptance evidence is committed.

- [x] Slices 1-7: domain, Conductor, immutable working-changes Subject, direct
  adapters, and fail-closed Review Records.
- [x] Slice 8: filesystem-backed Profiles, caller configuration, and explicit
  reviewer/model/effort provenance.
- [x] Slice 8a: Profile-owned reviewer judgment.
- [x] Slice 9: structured operational Review Record.
- [x] Slice 10 — structured canonical Findings (implemented locally).
- [x] Slice 11 — filesystem artifact evidence (implemented locally).
- [x] Slice 12 — SQLite ledger design and dependency decision.
- [x] Slice 13 — SQLite ledger plus minimal history.
- [x] DEV-56 — managed SQLite state and retired JSON compatibility (implemented locally).
- [x] DEV-57 — validated first-use onboarding contract (completed in Linear).
- [x] DEV-58 — deep internal Review Record projection (implemented locally).
- [x] DEV-59 — initialization and remembered managed-state selection (implemented locally).
- [x] DEV-60 — explicit Profile Creation and complete owned starter-set installation (implemented locally).
- [x] Slice 14 — history filters and operational queries (implemented locally).
- [x] Slice 15 — reproducible committed Review Subjects (implemented locally).
- [x] Slice 16 — replay of recorded experiment inputs (implemented locally).
- [x] Slice 17 — version-controlled eval corpus and ordinary Review execution (implemented locally).
- [x] Slice 17a — separate atomic canaries from the realistic general benchmark (implemented locally).
- [x] Slice 18 — human adjudication and basic scoring (implemented locally).
- [x] Slice 19 — experiment comparison (implemented locally).
- [x] DEV-66 — retry-aware Eval Suite execution hardening: DEV-67 lifecycle and
  checkpointing, then DEV-69 retries, then DEV-68 bounded concurrency.
  - [x] DEV-67 — durable progress and lifecycle complete
    (continue dogfood, do not reimplement).
  - [x] DEV-69 — finite retries under a frozen Experiment Configuration.
  - [x] DEV-68 — user-controlled bounded concurrency.
- [x] Slice 20 — seeded controlled defects (implemented locally).
- [x] Slice 26 — Party composition and Review Bundles (implemented locally;
  user-directed out of order ahead of Slices 21-25).
- [x] Slice 26a — layered Party composition: `extends` chains and
  `defaults.party` selection (implemented locally).
- [ ] Slice 21 — ACPX transport adapter.
- [ ] Slice 22 — native-versus-ACP adapter experiments.
- [ ] Slice 23 — fix verification and bounded continuation.
- [ ] Slice 24 — thin skill integration and migration.
- [ ] Slice 25 — supported local delivery baseline.

Current state: Slices 10 through 20, Slice 17a, Slices 26 and 26a, DEV-56
through DEV-60, and DEV-66 through DEV-69 are complete or implemented locally. Linear
owns future work selection; use Ready issues there before the older roadmap
below as execution authority.

## Dependency order

```text
Slice 9 operational facts
  ├──> Slice 10 structured Findings
  └──> Slice 11 artifact evidence
             │
             v
Slice 12 SQLite decision ──> Slice 13 ledger ──> Slice 14 history
                                      │
Slice 15 committed Subjects ──────────┤
             │                        │
             v                        v
Slice 16 replay                 Slice 17 eval execution
                                      │
                                      v
                              Slice 18 adjudication
                                      │
                                      v
                              Slice 19 comparison
                                      │
                                      v
                    DEV-66 Eval execution hardening
                                      │
                    DEV-67 → DEV-69 → DEV-68
                                      │
                                      v
                              Slice 20 seeded cases
```

Slices 21-25 follow the eval baseline. This deliberately makes the later
transport comparison consume the same queryable provenance, timing, completion,
and quality evidence as ordinary Reviews.

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
- An Eval Suite Run freezes its effective Retry Policy and
  per-run Concurrency Limit. A limit of one is the sequential default; higher
  values bound active Eval Cases without automatic resource sizing or a
  process-wide scheduler. Profile, Reviewer, model, effort, deadline, Retry
  Policy, and Concurrency Limit are durable experiment provenance.
- Retries reuse the same Eval Run and ordinary Review identity. Retry is not
  fallback, Reviewer substitution, or additional evaluation coverage; an
  exhausted case remains Incomplete and visibly affects suite completion.

## Slice execution protocol

For every unchecked slice:

1. Verify the branch, worktree, delivered commits, and prior slice acceptance
   evidence. Treat shipped behavior as reality when older prose disagrees.
2. Read the slice's named domain/design documents and inspect the listed likely
   touch points before changing code.
3. Change only that slice to **In progress** in this ledger. Keep later slices
   unchecked and do not opportunistically implement their abstractions.
4. Run `purposeful-test-design`, refine the slice's test-intent entry, and write
   only tests tied to plausible harmful defects.
5. Implement through the public or internal Interface named by the slice. Keep
   provider, SQL, filesystem, or Git mechanics behind their owning Module.
6. Run the smallest relevant tests, race checks where concurrency changed,
   `gofmt`, targeted `go vet`, targeted build checks, `git diff --check`, and all
   CodeScene gates required by `AGENTS.md`.
7. Perform the named real CLI, migration, or Agent Harness acceptance proof only
   when the slice requires it. Never call a live external run clean when it is
   incomplete.
8. Update this checklist, slice status, durable design/research documents, and
   README/CLI help in the same change set so a fresh agent sees the new reality.
9. Commit or deliver only with explicit user authorization. Record the commit or
   PR in the completed slice after delivery.

## Current implementation baseline — 2026-08-09

Review Party is a working experimental Go CLI with a synchronous, caller-first
`Conductor` Module. It currently provides:

- profile discovery and recipe explanation without Agent Harness launch or a
  Review Record;
- packaged, repository, and global filesystem-backed Markdown Profiles with
  deterministic precedence and no inheritance;
- `review-party review [PROFILE]` over one frozen working-changes
  Review Subject;
- explicit Grok, OpenCode, or Copilot Reviewer selection with no fallback;
- explicit caller-selected reasoning effort carried through the effective
  Profile Revision, actual adapter invocation where supported, and Review
  Record provenance;
- strict XDG user configuration for the Default Reviewer, Reviewer enablement,
  and per-Reviewer model selection/allowlists;
- direct CLI adapters with repository read/search-only capability backstops;
- finite Attempt deadlines, process-tree cleanup, and bounded diagnostics;
- canonical clean/findings validation with fail-closed Incomplete semantics;
- filesystem-backed Review Records and `review-party inspect`;
- human and JSON output with actual Reviewer provenance; and
- focused tests for subject freezing, lifecycle, adapter decoding, capability
  restrictions, result validation, persistence, and process cleanup.

The current implementation does not provide structured Finding values,
categorical termination facts, complete artifact retention, searchable history,
SQLite persistence, commit/branch/pull-request Subjects, replay, evals, retry or
fallback execution, Verification Reviews, Parties, ACPX transport, hosted
execution, or skill migration.

## Test-intent ledger for the active roadmap

Every implementation slice must refine its row before tests are written. Add
only tests whose fixture depends on the named rule; put migrations, live runs,
manual inspection, and one-time compatibility proof under acceptance checks.

| Behavior | Plausible defect | Boundary | Observable result | Decision |
| --- | --- | --- | --- | --- |
| Incomplete Reviews retain a typed cause and phase | An error string is persisted while its category is lost or misclassified | Conductor Review/Inspect Interface | JSON inspection distinguishes deadline, cancellation, availability, transport, and invalid-result failures | Test |
| Findings are machine-readable without scraping formatted output | The parser cross-associates a finding's evidence, location, correction, or regression-test intent | Canonical result parser | Returned `ReviewResult.Findings` contains the exact validated fields in source order | Test |
| Diagnostic artifacts support a Review without replacing its result | The record points to missing, partial, tampered, or root-escaping evidence | Real filesystem artifact implementation | Opening a recorded reference returns bytes matching its size and digest or a precise integrity error | Test |
| SQLite is authoritative for a persisted Review | A multi-table write partially succeeds or reconstructs a different Review Record | Real SQLite ledger implementation | A fresh ledger load returns the same aggregate; failed writes expose no partial Review | Test |
| History filters use effective recorded provenance | A query consults current configuration or profile defaults instead of the recorded run | Public CLI over real SQLite | History returns only records whose stored reviewer/profile/lifecycle matches the filter | Test |
| History queries remain bounded and operationally indexed | Invalid limits produce unbounded reads or a common Reviewer query scans and sorts the ledger | Real SQLite ledger implementation | Limits are rejected outside 1-200 and the measured Reviewer query uses its approved index | Test |
| A committed Subject is reviewed against its recorded head state | The diff is frozen but repository tools inspect the caller's newer working tree | Public Conductor Interface with a real temporary Git repository | The harness fixture reads the historical head content while the caller worktree remains unchanged | Test |
| A committed Subject execution is isolated and owned | Caller secrets leak through files/environment, or failure cleanup removes the wrong worktree | Subject execution Interface and public Conductor with real Git | Only committed files and allowed environment reach the Reviewer; owned terminal paths are removed and unrelated worktrees remain | Test |
| Replay reproduces inputs without claiming deterministic output | Replay recompiles the current Profile or silently substitutes a Reviewer | Public replay Interface | The new Review records the original Subject/Profile inputs and any explicit override, with a relation to the original | Test |
| Eval execution uses the ordinary Review path | An eval-only execution path bypasses capability, provenance, or incomplete-result rules | Public eval command with a scripted Reviewer | The Eval Run points to an ordinary inspectable Review Record with identical Conductor semantics | Test |
| Retry remains within one Eval Run | A failed Attempt creates a second Eval Run or its evidence is counted as independent coverage | Eval execution and ledger checkpoint boundary | Retry history remains attached to one Eval Run, only the final terminal result is adjudicated, and exhaustion produces one Incomplete case | Test |
| Eval concurrency is bounded per suite run | A user setting becomes a global worker pool, silently auto-sizes, or changes manifest identity | Public Eval Suite Run command with scripted Reviewers | The effective integer limit is persisted, one is sequential, higher values cap active cases, and completed records remain manifest-ordered | Test |
| Adjudication separates reviewer quality from execution failure | Incomplete cases are counted as missed Findings or clean results | Pure evaluator Module plus persisted adjudication | Recall/precision exclude incomplete cases and completion is reported separately | Test |
| Experiment comparison groups exact identities | Runs with different case revisions, builds, or Profile Revisions are silently pooled | Pure comparison Module over ledger fixtures | Incompatible groups are rejected or shown separately | Test |
| Seeded cases apply only the declared mutation | Fixture setup alters unrelated code or evaluates an unmutated checkout | Real temporary Git repository | The evaluated Subject identity contains the exact declared mutation and cleanup restores the source repository | Test |
| Party preflight fails closed before any launch | One invalid member lets earlier members launch then aborts mid-bundle | Public Conductor over real SQLite with scripted executors | Compilation failure returns an error, records zero attempts, and persists no Bundle row | Test |
| Party members share one frozen Subject identity | Per-member re-resolution lets a mid-party working-tree mutation change later Subjects | Public Conductor over a real temporary Git repository | Every child Review and the Bundle row record one identical pre-launch Subject identity | Test |
| Incomplete members stay visible in the Bundle | A completed member's findings are hidden or a partial Bundle claims clean | Public Conductor with one unavailable member | Bundle lifecycle is incomplete while the completed member keeps its review id and finding count | Test |
| Bounded party concurrency and manifest order | Limit>1 runs unbounded, or completion-order results misalign profile-to-review mapping | Public Conductor with blocking scripted executors | Peak active executions equal the effective limit and bundle members keep manifest order | Test |
| Bundles round-trip through SQLite | A partial multi-column write reconstructs a different aggregate | Real SQLite ledger implementation | A fresh load reproduces lifecycle, members, termination, and timestamps exactly | Test |

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

See [`design/profile-library-v1.md`](design/profile-library-v1.md).

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

## Slice 8 — Filesystem-backed Profiles and effective Reviewer selection

Status: **Complete**

### Goal

Expose user-recognized review behavior without conflating review intent with
Reviewer, model, Agent Harness, Transport, or caller policy.

### Completed checklist

- [x] Added the domain-backed `documentation` and `code-quality` Profiles
  beside `bugs`, including the maintainability eval corpus and multi-Profile
  Muse dogfood path.
- [x] Added packaged, repository, and global Markdown Profile libraries with
  deterministic precedence, complete-definition shadowing, and no inheritance.
- [x] Added `profiles`, `explain`, `profile explain`, and repository/global
  initialization behavior.
- [x] Added caller-owned Reviewer enablement, default selection, model choice,
  and model allowlists.
- [x] Carried an explicit reasoning-effort request through selection, Profile
  compilation, actual supported adapter invocation, and stored provenance.
- [x] Rejected unsupported explicit effort or model choices instead of silently
  dropping or substituting them.
- [x] Proved the OpenCode high-effort path with focused tests and a real stored
  Review Record.

### Acceptance evidence

- Two accepted Profiles produce meaningfully different, explainable behavior.
- Invalid higher-precedence Profile files and incompatible Reviewers fail before
  Agent Harness launch.
- Effective Profile Revision and Attempt provenance record the actual Reviewer,
  model, effort, harness, and transport.
- Delivered commits include PR #2 (`1841300`), PR #3 (`dc51ebf`), and PR #4
  (`2f908be`).

## Slice 8a — Profile-owned reviewer judgment

Status: **Complete**

Depends on: Slice 8.

### Goal

Make a Review Profile the complete, inspectable definition of purpose-specific
Reviewer Judgment while keeping Review Party's enforceable execution and result
guarantees behind the profile-library Module.

### Completed checklist

- [x] Moved the mature material-bug methodology into packaged
  `profiles/bugs.md`, including materiality, evidence, confidence, root-cause,
  false-positive, test-gap, and subject-sensitive risk guidance.
- [x] Compiled packaged Profiles through the same filesystem-backed path used
  by repository and global Profiles so Markdown remains the single judgment
  source.
- [x] Preserved compiler-owned capability restrictions, Context Discovery,
  immutable Review Subject framing, canonical Review Result validation,
  provenance, deadlines, and incomplete-result semantics.
- [x] Proved that a repository override replaces packaged judgment as a whole
  while retaining the compiler-owned guarantee kernel.
- [x] Kept the authored Profile and effective compiler recipe inspectable
  through the existing Profile explanation and Review Record surfaces.

### Non-goals and stop rule

- Do not make Pass graphs, Recovery Policy, capabilities, result parsers,
  commands, Agent Harnesses, or Transports user-defined through prompt prose.
- Do not introduce shared hidden judgment fragments inherited by every
  Profile.
- Keep the deferred Profile-authoring companion skill in Linear issue DEV-51,
  not in this repository execution plan.
- Do not add configurable orchestration fields or reusable prompt inheritance
  without two demonstrated Profile recipes that require the new seam and a
  separate accepted design.

### Acceptance evidence

- The packaged `bugs` prompt contains the accepted material-review judgment
  rules and targeted risk priorities.
- A repository `bugs.md` receives Subject framing and the canonical result
  contract without inheriting packaged bug-review judgment.
- Packaged and custom Profiles compile through one prompt-assembly path.
- Focused Go tests, formatter, vet, build, and CodeScene checks passed in PR #4
  (`2f908be`).

## Slice 9 — Structured operational Review Record

Status: **Complete**

Depends on: Slice 8a.

### Goal

Make one inspected Review Record answer what ran, which build ran it, how long
its meaningful phases took, and why an incomplete Review stopped without
turning execution phases into new Review Lifecycle states.

### Interface and ownership decisions

- Keep `Pending`, `Running`, `Completed`, and `Incomplete` as the only Review
  Lifecycle values.
- Represent an incomplete stop with a typed termination fact containing a
  stable category, execution phase, and bounded human diagnostic.
- Keep `AttemptOutcome` as the normalized outcome of an actual harness launch.
  An Availability Check can terminate a Review without inventing an Attempt.
- Record build provenance once per Review: release version when present, VCS
  revision when available, and whether the binary was built from modified
  source when Go build information can establish it.
- Record elapsed phase measurements as data on the Review Record. Do not create
  an event-sourced lifecycle or require replaying events to recover state.
- Introduce an explicit Review Record schema version and a load-time compatibility
  path for existing unversioned JSON records.

### Implementation checklist

- [x] Use `purposeful-test-design` to refine the Slice 9 row in the test-intent
  ledger before editing tests.
- [x] Define the stable termination categories. At minimum distinguish reviewer
  unavailable, authentication failure, deadline exceeded, cancellation,
  transport failure, malformed output, result validation failure, and unknown
  failure.
- [x] Define stable phase names for the phases Review Party actually owns. Do
  not expose provider-native internal phases.
- [x] Add runtime/build provenance collection without shelling out to Git during
  every Review.
- [x] Capture total duration plus the useful owned phases: Subject resolution,
  Profile compilation, Availability Check, Attempt execution, result decoding
  and validation, and record persistence where measurable without circular
  bookkeeping.
- [x] Add cheap Subject facts such as changed-file count and diff additions and
  deletions at Subject resolution rather than reparsing terminal output later.
- [x] Replace new writes of free-form `IncompleteCause` with the typed
  termination representation while retaining backward inspection for old JSON.
- [x] Make human inspection concise and JSON inspection complete.
- [x] Confirm that no new domain term is introduced; `CONTEXT.md` remains
  unchanged because ordinary phases and diagnostics are not domain concepts.

### Smallest purposeful test set

- [x] A classification table distinguishes deadline, caller cancellation,
  availability, transport, malformed-output, validation, and unknown failures.
- [x] An unavailable Reviewer records no Attempt but retains an
  availability-phase termination.
- [x] A controlled-clock Review records non-negative owned-phase timings and a
  coherent total duration.
- [x] A representative current unversioned JSON fixture remains inspectable
  through the compatibility path.
- [x] Build provenance records only version/revision/modified values established
  by supplied Go build information.
- [x] A real temporary-Git Subject records changed-file, addition, deletion, and
  binary-file facts for tracked and untracked changes.

### Acceptance checklist

- [x] `review-party inspect <id> --format json` contains complete operational
  facts without scraping human output.
- [x] Categorical counts can be computed from records without parsing error
  strings.
- [x] Existing JSON Review Records remain readable.
- [x] Focused package tests, race tests for changed concurrent code, `go vet`,
  `gofmt`, `git diff --check`, and the required CodeScene checks pass.
- [x] No observability backend, event stream, or new dependency is introduced.

### Local verification evidence

- `go test . ./cmd/review-party`
- `go test -race . ./cmd/review-party`
- `go vet . ./cmd/review-party`
- `go build -o scratch/review-party ./cmd/review-party`
- `git diff --check`
- CodeScene pre-commit safeguard: passed with no findings. All new source files
  score at least 9.0; touched existing files do not regress.
- A real pre-schema-version Review Record was inspected through the CLI and
  normalized to schema version 1 without inventing runtime, timing, or Subject
  facts.

Record persistence is included in total elapsed time to the extent it can be
measured before the final write. It is not exposed as a separate self-referential
field because persisting that measurement would itself require another write.

### Stop rule

Do not add OpenTelemetry, metrics exporters, dashboards, or hosted log shipping.
The Review Record is the local execution trace for this phase.

### Likely touch points

`types.go`, `subject_git.go`, `conductor.go`, `executor.go`, `store.go`,
`cmd/review-party/main.go`, and their focused tests.

## Slice 10 — Structured canonical Findings

Status: **Implemented locally**

Depends on: Slice 9.

### Goal

Make canonical Findings available as structured domain values so callers,
history, replay, and evals never have to scrape `ReviewResult.Raw`.

### Interface and ownership decisions

- A `Finding` remains an evidence-bearing Reviewer claim, not an accepted fact.
- The result parser owns translation from the canonical text contract into
  structured Findings.
- `ReviewResult.FindingCount` is derived from the Findings collection rather
  than becoming a second authority.
- Preserve the exact validated canonical block as evidence and for backward
  inspection.
- Bump the result-contract revision. A Profile Revision using the old contract
  must remain distinguishable from one using the structured contract.

### Implementation checklist

- [x] Refine the Slice 10 test-intent row before writing tests.
- [x] Define `Finding` fields for ordinal or stable within-result identity,
  severity, category, location, failure, evidence, smallest safe correction,
  and regression-test intent.
- [x] Decide and document whether location is represented as one validated
  string or separate path/line fields. Do not guess at language-specific spans.
- [x] Parse every validated finding section into exactly one structured value.
- [x] Reject duplicated, missing, or cross-associated fields.
- [x] Render human output and JSON from the same structured result.
- [x] Preserve backward loading of old count/raw-only Review Records.

### Smallest purposeful test set

- A two-finding result preserves each finding's location and evidence without
  crossing section boundaries.
- Missing `Evidence:` or duplicated `Test:` remains invalid.
- Clean output produces an empty Findings collection.
- JSON round-trip preserves the structured result and derives the same count.

### Acceptance checklist

- [x] A caller can enumerate Findings from inspected JSON without parsing text.
- [x] Existing canonical-v1 records remain inspectable.
- [x] A real bounded Review demonstrates structured Findings or a structurally
  valid clean collection.
- [ ] Focused verification and CodeScene gates pass.

Local acceptance evidence (uncommitted): OpenCode Muse
`meta/muse-spark-1.2-contributor` completed bounded Bugs Review
`rp_1786417505648_4ffb4ccf9e7eed00` on 2026-08-10. Its persisted `inspect`
JSON recorded `canonical-v2`, `status: clean`, `finding_count: 0`, and
`findings: []`.

### Stop rule

Do not add semantic deduplication, Finding lineage, LLM matching, acceptance
state, or remediation authority.

### Likely touch points

`types.go`, `result_contract.go`, `result_parser.go`, `conductor.go`, CLI
rendering, and parser/Conductor tests.

## Slice 11 — Filesystem artifact evidence

Status: **Implemented locally**

Depends on: Slices 9-10.

### Goal

Preserve bounded native harness evidence outside the structured Review Record so
an operator can distinguish Reviewer failure, adapter decoding failure, and
result-contract failure without turning large diagnostics into the primary
result.

### Interface and ownership decisions

- SQLite will eventually own structured facts; the filesystem owns potentially
  large artifacts.
- Use one concrete filesystem artifact Module initially. Do not add a storage
  adapter seam merely to mock it; exercise it against temporary directories.
- Store references using paths relative to the configured Review Party state
  root, plus kind, size, digest, and truncation status.
- Distinguish native stdout/stderr, decoded assistant text, constructed prompt,
  and other diagnostic kinds. Do not label decoded assistant text as the full
  raw harness transcript.
- Use private permissions and bounded capture. Artifact retention must not
  weaken existing output limits.

### Implementation checklist

- [x] Refine the Slice 11 test-intent row before writing tests.
- [x] Design the on-disk layout under the existing configured record/state root,
  for example `artifacts/<review-id>/<attempt-number>/...`.
- [x] Add an `ArtifactReference` to the relevant Attempt Record.
- [x] Publish via a temporary file, sync where required by the current record
  durability contract, restrict permissions, then atomically rename.
- [x] Record a digest and byte count over the published bytes.
- [x] Define cleanup behavior when artifact publication succeeds but Review
  Record publication fails. Do not delete pre-existing user artifacts.
- [x] Add an inspection path that reports references and integrity failures;
  avoid printing large artifacts by default.
- [x] Document retention and sensitivity in the README.

### Smallest purposeful test set

- Published bytes reopen with the recorded size and digest.
- A tampered or missing artifact produces a precise integrity/not-found error.
- A forced record-save failure does not leave a Review claiming an unpublished
  artifact.
- Oversized harness output remains bounded and records truncation honestly.

### Acceptance checklist

- [x] Decoder failure evidence is inspectable without parsing the database or
  record file manually.
- [x] Artifact paths cannot escape the configured root through traversal or
  symlinks.
- [x] Record JSON remains reasonably sized.
- [x] Focused filesystem, Conductor, and CLI tests plus CodeScene gates pass.

### Stop rule

Do not add compression, content-addressed global deduplication, remote object
storage, retention daemons, or artifact search.

### Likely touch points

New focused artifact implementation and tests, `types.go`, `executor.go`,
`conductor.go`, `store.go`, CLI inspection, and README documentation.

## Slice 12 — SQLite ledger design and dependency decision

Status: **Decision gate**

Depends on: Slices 9-11.

### Goal

Select an explicit local SQLite architecture that earns its dependency cost and
preserves Review Party's deep Conductor Interface, local-first behavior, and
non-destructive compatibility with existing JSON Review Records.

### Required decisions

- [ ] Research current SQLite driver choices from primary documentation at the
  time of implementation. Compare pure-Go and CGO options, supported platforms,
  binary size, maintenance, transaction behavior, and transitive dependency
  impact. Treat invoking an external `sqlite3` process as a separate runtime
  dependency, not as a standard-library solution.
- [ ] Obtain explicit user approval before adding the selected Go module or
  external runtime requirement.
- [ ] Write `docs/design/sqlite-ledger-v1.md` with the chosen Interface,
  migration ownership, state-directory layout, busy/locking policy, durability
  settings, corruption behavior, and import/rollback story.
- [ ] Decide how the existing `Config.RecordDirectory` and `--records` behavior
  selects the ledger plus artifact root without silently abandoning records.
- [ ] Design versioned embedded migrations. Application startup may migrate
  forward; it must never improvise schema from current structs.
- [ ] Define a non-destructive, idempotent import of existing JSON records.
- [ ] Decide the minimum relational projection. It must preserve Reviews,
  Passes, Attempts, structured Findings, artifact references, immutable Subject
  identity, effective Profile Revision, and build provenance.
- [ ] Keep benchmark definitions version-controlled on disk; only executions,
  adjudications, and aggregate experiment facts belong in the local ledger.

### Schema constraints

- SQLite contains current durable facts. Optional logs explain how they
  happened; events are not replayed to reconstruct canonical state.
- One transaction publishes a logically complete Review aggregate.
- Do not maintain both a serialized full Review Record and normalized columns as
  competing authorities unless the design explains and tests the projection
  invariant.
- Large prompts, native output, and debug material remain filesystem artifacts.
- Foreign keys, uniqueness, and migration versioning are enabled explicitly.

### Acceptance checklist

- [ ] The user has approved the concrete dependency choice.
- [ ] The design can reconstruct the public Review Record without knowledge of
  SQL in the CLI or Conductor callers.
- [ ] Existing JSON records have a non-destructive import and rollback path.
- [ ] The design states what happens on concurrent CLI access, a busy database,
  disk-full errors, and migration failure.
- [ ] No source implementation begins while any item above is unresolved.

### Stop rule

If no dependency option satisfies the supported-platform and operational
requirements, stop and retain the filesystem store. Do not hide a fallback to a
different database, shell command, or in-memory history.

## Slice 13 — SQLite Review Ledger and minimal history

Status: **Implemented locally**

Depends on: approved Slice 12.

### Goal

Make SQLite the authoritative local ledger for new structured Review facts and
prove its user value with the smallest history command while leaving large
artifacts on disk.

### Interface and ownership decisions

- Replace the shallow `Save`/`Load` file implementation with a ledger Module
  that hides transactions, migrations, relational reconstruction, and imports.
- Keep the Conductor caller-first. `Review` and `Inspect` must not expose SQL,
  rows, transactions, or migration concerns.
- Use the real SQLite implementation in persistence tests. Do not create a fake
  adapter whose behavior cannot prove constraints or transactionality.
- Import legacy JSON once, idempotently, and leave source files untouched.

### Implementation checklist

- [ ] Refine the SQLite row in the test-intent ledger before tests.
- [ ] Add the approved driver and record the dependency decision.
- [ ] Embed and apply schema migrations with a finite busy timeout and explicit
  foreign-key enforcement.
- [ ] Implement transactional creation and update of Reviews, Passes, Attempts,
  Findings, terminations, and artifact references.
- [ ] Reconstruct the same public `ReviewRecord` used by JSON output.
- [ ] Add idempotent legacy JSON import with per-record success/failure reporting.
- [ ] Preserve `inspect <id>` behavior across a fresh process.
- [ ] Add `review-party history --limit N` ordered deterministically by creation
  time and Review ID.
- [ ] Keep old JSON files and provide a documented rollback path until the local
  release baseline is accepted.

### Smallest purposeful test set

- A completed and an incomplete Review round-trip through a fresh database
  connection with identical public facts.
- A forced child-row failure rolls back the entire Review aggregate.
- The same legacy JSON import can run twice without duplicates.
- Two Reviews with identical timestamps have deterministic history ordering.
- A schema newer than the binary fails explicitly rather than being modified.

### Acceptance checklist

- [ ] Real CLI `review`, fresh-process `inspect`, and `history` work from one
  temporary state root.
- [ ] Filesystem artifacts referenced by the ledger pass integrity checks.
- [ ] Concurrent readers and the supported writer pattern behave according to
  the approved design.
- [ ] Focused database tests, `go vet`, build, formatting, diff, dependency
  audit, and CodeScene gates pass.
- [ ] A dated local migration rehearsal records counts of imported, skipped,
  and failed legacy records without deleting any source file.

### Stop rule

Do not add rich filters, replay, eval tables, lineage, remote synchronization,
or a generic repository framework in this slice.

### Likely touch points

The ledger implementation and embedded migrations, `store.go` replacement or
deepening, `conductor.go`, configuration and CLI construction, persistence/CLI
tests, README, and the SQLite design document.

## Slice 14 — History filters and operational queries

Status: **Implemented locally**

Depends on: Slice 13.

### Goal

Answer the first relational operational questions without exposing arbitrary
SQL or turning Review Party into a dashboard.

### Public behavior

```text
review-party history
review-party history --repo PATH
review-party history --reviewer grok
review-party history --profile bugs
review-party history --lifecycle incomplete
review-party history --termination deadline_exceeded
review-party history --subject <identity>
review-party history --since <timestamp>
```

Human output is concise; JSON output includes stable structured summaries and
pagination/limit facts.

### Implementation checklist

- [x] Define one typed `HistoryQuery` rather than leaking independent SQL
  fragments through CLI callers.
- [x] Define deterministic ordering, finite default/max limits, and timestamp
  parsing.
- [x] Filter on effective stored provenance, never current configuration.
- [x] Resolve `--repo` to the same canonical repository identity used in Review
  Subjects.
- [x] Keep incomplete Reviews and their termination facts visible.
- [x] Add useful indexes only from measured query plans over representative
  fixtures; do not pre-emptively index every column.
- [x] Document the difference between history and replay.

### Smallest purposeful test set

- Reviewer/Profile filters use recorded effective values even after current
  configuration changes.
- Combined filters apply conjunctively and preserve deterministic ordering.
- Limit bounds prevent an unbounded result set.
- Human and JSON forms describe the same selected records.

### Acceptance checklist

- [x] The CLI answers the named queries without manually opening SQLite.
- [x] Query plans for representative history sizes avoid an obvious full scan
  where an approved index should apply.
- [ ] Focused ledger and CLI verification pass; CodeScene is unavailable in this environment.

### Stop rule

Do not add arbitrary SQL execution, saved searches, dashboards, Finding lineage,
or cross-machine synchronization.

## Slice 15 — Reproducible committed Review Subjects

Status: **Implemented locally**

Depends on: Slices 9-14.

### Goal

Review an exact committed base/head range while ensuring both the frozen diff
and repository tools observe the recorded head state.

### Interface and ownership decisions

- Add an explicit committed-range Subject kind; do not reinterpret the existing
  working-changes Subject.
- Store repository identity, base and head object IDs, changed paths, diff
  payload/identity, and Subject size facts.
- Treat the isolated checkout as a source-view sandbox that provides version
  isolation, not as a provisioned developer environment or security sandbox.
  Its purpose is to make repository reads and searches observe the same
  recorded head state as the frozen base/head diff.
- Execute the Agent Harness in a temporary detached worktree or equivalently
  isolated repository view at the recorded head. Merely storing SHAs while
  pointing tools at the caller's current working tree is invalid.
- Put temporary repository preparation and cleanup behind one small Subject
  execution Interface. Keep worktree ownership, process ordering, exact-path
  validation, and recovery inside that Module rather than exposing Git
  lifecycle operations through Reviewer adapters.
- Do not install dependencies or copy, mount, or symlink the caller's
  `node_modules`, ignored files, or untracked files. Package preparation is a
  separate future execution capability, not an implicit part of committed
  Subject resolution.
- Launch the Reviewer with an adapter-owned environment policy rather than the
  caller's unrestricted process environment. Record policy and variable names
  where useful, never values, and do not discover or import project `.env`
  files. Committed environment files remain ordinary committed source.
- Give each temporary worktree an explicit owned lifecycle and clean it after
  every terminal outcome. Reconcile only inactive Review Party-owned leftovers
  after crashes; Git pruning is recovery for stale administrative records, not
  the normal cleanup mechanism.
- V1 uses commits already present in a local repository. Remote fetching and
  pull-request resolution remain separate future Subjects.

### Proposed CLI behavior

```text
review-party review bugs --repo PATH --base <commit> --head <commit>
```

Reject ambiguous combinations of working changes and committed-range flags.

### Implementation checklist

- [x] Refine the committed-Subject test-intent row before tests.
- [x] Design the SubjectReference addition without putting Git commands into
  domain types.
- [x] Resolve both revisions to full object IDs before creating the Review.
- [x] Capture the immutable diff and deterministic identity with argv-based,
  repository-scoped Git commands.
- [x] Create a bounded temporary worktree under `scratch/` for tests and a
  documented Review Party-owned runtime location for real execution. Create it
  detached at the resolved full head object ID with unique Review/Attempt
  ownership metadata outside the checkout.
- [x] Ensure prompt construction and repository tool access use the isolated
  head checkout.
- [x] Prove that preparation performs no package-manager install and does not
  copy or link dependency trees, ignored `.env` files, credentials, or other
  caller worktree state.
- [x] Define the minimum environment required by each Reviewer adapter and stop
  inheriting unrelated caller variables. Preserve required Reviewer
  authentication without persisting secret values.
- [x] After the Reviewer process tree has stopped, remove only the exact owned
  temporary worktree created by the run on success, Incomplete outcome,
  cancellation, timeout, launch failure, and recoverable panic; never mutate or
  clean the caller's working tree.
- [x] Add bounded startup reconciliation for inactive Review Party-owned
  worktrees left by crashes. Match stable Git worktree metadata and ownership
  records rather than guessing from paths or age alone.
- [x] Record and report cleanup failure without rewriting a valid reviewer
  result as clean or losing diagnostic evidence.

### Smallest purposeful test set

- After committed Subject resolution, advancing the caller branch and editing
  its working tree cannot alter the patch or repository content observed by the
  scripted harness.
- Dependency directories and ignored `.env` files present in the caller's
  working tree are absent from the committed Subject worktree, while committed
  files remain visible.
- A scripted Reviewer receives its declared environment allowlist but not an
  unrelated sentinel secret from the caller environment.
- Invalid or missing base/head objects prevent launch.
- The source repository's branch, index, and working tree are unchanged after
  successful and cancelled runs.
- Success, Incomplete output, launch failure, cancellation, and timeout all
  remove the owned worktree after child-process cleanup; a simulated crash
  leftover is recovered without touching an unrelated user worktree.
- Binary and renamed-file changes follow an explicit recorded rule.

### Acceptance checklist

- [x] A real local historical commit range completes through the same public
  Review command.
- [x] Inspected provenance contains full base/head IDs and Subject identity.
- [x] The recorded diff and every repository read/search performed by the
  harness observe the same head commit without provisioning application
  dependencies or importing caller secrets.
- [x] No Review Party-owned temporary worktree remains after an ordinary
  terminal outcome, and crash recovery is bounded to verified owned entries.
- [x] No live Agent Harness is needed for deterministic repository-state tests.
- [x] Focused Git, Conductor, process-cleanup, and CLI tests plus CodeScene
  gates pass.

Acceptance evidence (2026-08-11): final isolated-state CLI Review
`rp_1786474858573_cf05d154085714c8` completed with Findings through OpenCode
Muse `meta/muse-spark-1.2-contributor` in 78 seconds. Fresh-process inspection
verified full base/head IDs, `canonical-v2`, direct-CLI Reviewer provenance,
and artifact integrity. Git and the owned runtime root showed no leftover
Review Party worktree. One validated out-of-scope data-integrity concern was
captured in Linear; the run was not reported as clean.

### Stop rule

Do not add branch tracking, network fetch, GitHub PR resolution, patch
application to a dirty worktree, dependency provisioning, reusable worktree
pools, or long-lived checkout management. If a future Profile needs project
build or test execution, design an explicit capability with locked dependency
preparation, package-manager-owned cache/store reuse, environment provenance,
and separately authorized network and lifecycle-script behavior.

## Slice 16 — Replay recorded experiment inputs

Status: **Implemented locally**

Depends on: Slices 13 and 15.

### Goal

Create a new ordinary Review from a prior Review's frozen experiment inputs
without claiming deterministic model output or replaying application events.

### Public behavior

```text
review-party replay <review-id>
review-party replay <review-id> --reviewer <id> --model <model> --effort <effort>
```

The default reuses the original committed Subject and effective Profile
Revision snapshot. Explicit overrides produce recorded differences; no
unavailable original choice is silently replaced.

### Interface and ownership decisions

- Add a Review relationship such as `replays_review_id`; the new Review retains
  its own ID, lifecycle, attempts, result, and build provenance.
- Reuse the stored Profile Revision snapshot rather than recompiling the current
  file with the same Profile name.
- Validate that the recorded repository and commits remain locally available
  before launch.
- Working-changes Reviews are not replayable until a separate design proves how
  to reconstruct their exact repository view safely.

### Implementation checklist

- [x] Refine the replay test-intent row before tests.
- [x] Add a small Conductor replay Interface that hides ledger lookup, input
  reconstruction, relation creation, and execution.
- [x] Define allowed overrides and ensure each participates in the new effective
  provenance.
- [x] Reject silent model, effort, transport, capability, or Profile
  substitution.
- [x] Show original/replay linkage in inspect and history JSON.
- [x] Preserve both records independently.

### Smallest purposeful test set

- Changing the on-disk Profile after the original Review does not change a
  default replay's stored Profile Revision.
- An explicit Reviewer override is recorded; an unavailable original Reviewer
  remains honestly incomplete without fallback.
- Original and replay share input identity but have different Review IDs,
  attempts, timestamps, and runtime provenance.
- A working-changes source Review is rejected with a precise unsupported replay
  diagnostic.

### Acceptance checklist

- [x] A real replay is inspectable and related to its source Review.
- [x] Documentation states that replay reproduces inputs, not model output.
- [x] Focused Conductor, ledger, CLI, and Git tests plus CodeScene gates pass.

Acceptance evidence (2026-08-11): isolated-state source Review
`rp_1786479504096_ce23c58a2d27e5e0` and replay
`rp_1786479549553_220f41f573558625` both completed with Findings through
OpenCode Muse `meta/muse-spark-1.2-contributor`. Fresh-process inspect and
history confirmed the replay's distinct ID, `replays_review_id` lineage, and
the exact frozen committed Subject identity and Profile Revision. The run is
evidence of input reconstruction and persisted lineage, not deterministic
model output. Focused race tests, vet, build, formatting, and the final
CodeScene pre-commit safeguard passed with no findings.

### Stop rule

Do not add event sourcing, result overwrites, implicit latest-model upgrades, or
cross-machine repository recovery.

## Slice 17 — Version-controlled eval corpus and ordinary Review execution

Status: **Implemented locally**

Depends on: Slices 10, 13, and 15. Slice 16 is useful but not required by the
implementation.

### Goal

Run a small, human-curated benchmark through the exact Conductor path used by
ordinary callers and persist an Eval Run related to an ordinary Review Record.

### Corpus contract

- Ship an immutable `global:general-bugs` baseline and accept explicit
  Caller-owned suite paths without treating either corpus as product policy.
- Each case has a stable ID, schema version, case revision/digest, change/state
  mode, classification, fixture content, and expected Findings.
- Expected Findings include human-readable defect identity, material behavior,
  supporting evidence, and expected file/location where reliable.
- Keep Reviewer/model/effort/deadline, Retry Policy, and Concurrency Limit in a
  separate Experiment Configuration. Freeze the effective values into the Eval
  Suite Run rather than consulting mutable caller configuration later.
- Materialize a Git-free Synthetic Review Subject without corpus answers,
  source paths, commit identities, or fixing commits.
- Include both defect-containing and known-clean cases from the first usable
  anonymous, independently authored corpus.
- Another model's Findings may help discover candidates but never become the
  gold set without human validation.

### Relationship

```text
Eval Suite Run
└── Eval Run
    ├── Eval Case Revision
    └── ordinary Review Record
```

There is no `EvalReview`. The Eval runner calls the public Conductor Interface
and preserves all ordinary capability, deadline, provenance, artifact, and
incomplete-result semantics.

### Initial CLI behavior

```text
review-party eval run <suite>
review-party eval inspect <eval-run-id> --format human|json
```

The first run may be `awaiting_adjudication`; execution and scoring are separate
facts.

### Implementation checklist

- [x] Refine the eval-execution test-intent row before tests.
- [x] Design the case schema and validate unknown fields, duplicate IDs,
  unsupported versions, missing commits, and contradictory clean/expected
  Finding declarations before launching a Reviewer.
- [x] Start with four independently authored defect cases and one realistic
  known-clean case; expand language and defect coverage through versioned suite
  revisions.
- [x] Reclassify the original one-file cases as `global:canary-bugs` after
  post-run adjudication showed they were useful plumbing checks but produced a
  model-capability ceiling effect.
- [x] Publish `global:general-bugs@general-bugs-v2` with multi-file context,
  cross-file or lifecycle reasoning, four defect cases, and two adversarial
  known-clean ownership changes.
- [x] Freeze the case revision/digest into every Eval Run.
- [x] Execute each case through `Conductor.Review` with explicit Profile,
  Reviewer, model, and effort selection.
- [x] Persist Eval Run execution state and its ordinary Review ID in SQLite.
- [x] Keep completion statistics separate from adjudication and quality scores.
- [x] Establish sequential, manifest-ordered execution as the initial baseline.
  Configurable bounded concurrency and retry-aware lifecycle handling are the
  approved DEV-66 follow-up rather than an implicit Slice 17 behavior.

### Smallest purposeful test set

- An invalid case prevents all harness launch for that case.
- A scripted clean, findings, and incomplete case each produce an ordinary
  Review Record and the correct Eval Run execution state.
- The Eval Run records the exact case digest, Profile Revision, Reviewer/model/
  effort, and Review Party build through its related Review.
- A clean case cannot declare expected Findings.

### Acceptance checklist

- [x] One command runs the initial local suite and every case has an inspectable
  ordinary Review Record.
- [x] Rerunning a case creates a new Eval Run rather than overwriting history.
- [x] No scoring requires terminal-output scraping.
- [x] Focused case-schema, Conductor, ledger, and CLI tests plus CodeScene gates
  pass.
- [x] A dated dogfood run records completion categories and runtime before any
  prompt comparison is claimed.

Acceptance evidence (2026-08-11): Eval Suite Run
`esr_1786488281874_5ab628f00728dcbf` executed the immutable
`global:general-bugs@general-bugs-v1` suite through OpenCode Muse
`meta/muse-spark-1.2-contributor` at high effort. Its five sequential ordinary
Reviews completed in 99 seconds: all four defect fixtures returned Findings,
the known-clean fixture returned clean, and none were Incomplete. Each Eval Run
remains `awaiting_adjudication`; this is execution evidence, not a score.
Fresh-process inspection verified `captured-change` Subjects with synthetic
`eval://` identities, no Git objects or source paths, exact Profile/Reviewer
provenance, `canonical-v2`, and valid artifacts. Focused race tests, vet, build,
formatting, and CodeScene gates passed.

Post-adjudication correction (2026-08-12): the v1 fixtures are now packaged as
`global:canary-bugs@canary-bugs-v1`; their historical Eval Suite Run remains
immutable under its originally recorded name. They are explicitly unsuitable
for broad capability claims because every repository was a one-file micro-case
and the full `bugs` Profile strongly cued each risk category. The replacement
`global:general-bugs@general-bugs-v2` uses six anonymous multi-file cases: four
defects that require persistence, tenant, or worker-contract reasoning and two
known-clean ownership transfers designed to punish superficial pattern
matching. Semantic scoring remains limited to validated Review Results;
result-contract failures stay separately visible as Incomplete operational
facts.

Acceptance evidence (2026-08-12): both exact experiments executed immutable
suite digest `1b0e585fbfe01d7387dd4c6270fa6b5d44d74be10778376297ddce7e5c0ff545`.
Muse Eval Suite Run `esr_1786548083898_fc7ce7fa4a119434` completed all six
cases in 158 seconds; Adjudication Revision
`ar_1786548279989_348d317c395f5693` recorded 4/4 recall, 4/6 precision after
two duplicate test-gap Findings were rejected, 2/2 clean accuracy, and 6/6
completion. DeepSeek V4 Flash Eval Suite Run
`esr_1786548288441_1633f2e80772ddca` completed in 267 seconds; Adjudication
Revision `ar_1786548583824_61615d51e31a6e0e` recorded 4/4 recall, 4/4
precision, 1/1 scored clean accuracy, and 5/6 completion. Its remaining clean
case reasoned correctly in the raw artifact but failed the exact clean-result
contract, so it remains an unscored `result_validation_failure`. These are
paired experiment inputs, not a winner declaration; formal comparison remains
Slice 19.

### Approved execution-hardening follow-up

DEV-66 extends this baseline without creating an Eval-only execution path:

- DEV-67 creates the complete Eval Suite Run and Eval Run manifest before the
  first Reviewer launch, persists explicit parent and case lifecycle facts, and
  checkpoints each case atomically with its parent progress.
- DEV-69 adds a finite Retry Policy. A retry uses the same Eval Case, synthetic
  Subject, Experiment Configuration, Eval Run, and ordinary Review identity;
  only the final terminal Review Result is eligible for adjudication.
- DEV-68 adds a caller-selected integer Concurrency Limit scoped to one Eval
  Suite Run. The default is one, higher values bound active cases, backoff does
  not occupy a slot, and completion/reporting remain manifest-ordered.
- Suite cancellation, hard persistence failure, or the suite-wide deadline
  stops new work while active cases terminate honestly. Exhausted cases remain
  Incomplete; completed cases and their evidence are retained.

Slices 18-25 must consume this contract rather than reimplementing retry,
concurrency, or parent lifecycle behavior in their own modules.

### Stop rule

Do not add LLM judges, generated repositories, giant benchmark imports,
statistical-significance machinery, hosted eval platforms, automatic resource
sizing, provider-capacity prediction, or a process-wide Eval scheduler.

## Slice 18 — Human adjudication and basic scoring

Status: **Implemented locally**

Depends on: Slice 17. New adjudication revisions must consume the finalized
DEV-66 retry and lifecycle contract; the existing local revision evidence
remains immutable.

### Goal

Let a human map reported Findings to expected defects, classify novel Findings,
and compute simple quality metrics without confusing infrastructure failure with
Reviewer quality.

### Scoring contract

- Expected Finding: matched or missed.
- Reported Finding: matched expected, novel valid, or false positive.
- Defect-case recall: matched expected Findings divided by expected Findings.
- Precision: matched plus novel valid Findings divided by all reported Findings.
- Clean-case behavior: report false-positive count/rate and clean-case accuracy
  explicitly.
- Completion rate and termination categories are reported separately.
- Incomplete cases are not silently counted as clean or ordinary misses. Show
  them as unscored incomplete executions.
- Retry Attempts remain one Eval Run and do not create additional adjudication
  coverage. Only the final terminal result is scored; exhausted retries remain
  unscored Incomplete executions with their termination category.

### Human interface checkpoint

Before implementation, choose and document one small human-friendly
adjudication workflow plus a machine-readable import/export form. Do not expose
SQLite row IDs or require hand-written SQL. The CLI must show stable case,
expected-Finding, and reported-Finding identities rather than package-internal
keys.

### Implementation checklist

- [x] Refine the adjudication test-intent row before tests.
- [x] Define immutable adjudication revisions so corrections are auditable and
  do not rewrite original Review Results.
- [x] Require every reported Finding to receive exactly one disposition before
  a run is fully scored.
- [x] Permit explicit unscored/uncertain adjudication without guessing.
- [x] Calculate metrics in a pure evaluator Module and persist inputs plus
  outputs transactionally.
- [x] Render per-case evidence before suite aggregates.
- [x] Add `review-party eval score` or the selected equivalent without adding a
  dashboard.
- [x] Reconcile adjudication import/export and persistence with DEV-66 so retry
  Attempts, exhausted cases, parent termination, and retained completed cases
  remain visible without double-counting coverage.

### Smallest purposeful test set

- One expected match, one miss, one novel valid Finding, and one false positive
  produce the exact stated recall and precision.
- A clean Review with zero Findings scores clean; a clean case with a reported
  false positive does not.
- An incomplete Review remains unscored and lowers completion rate without
  being counted as a missed defect.
- Duplicate or incomplete dispositions cannot publish a final score.
- A revised adjudication preserves the earlier revision and produces a new
  aggregate.

### Acceptance checklist

- [x] A human can adjudicate the seed corpus without opening the database.
- [x] The stored decisions explain every numerator and denominator.
- [x] JSON output is sufficient for automation and later comparison.
- [x] Focused evaluator, ledger, and CLI tests plus CodeScene gates pass.

Acceptance evidence (2026-08-11): the exported seed-corpus documents were
human-adjudicated and published as immutable revisions for both the configured
Muse run and the exact OpenCode Go DeepSeek V4 Flash run. Each stored score
retains case-level decisions, explicit numerator/denominator pairs, uncertain
judgments, incomplete cases, and termination-category counts. Muse revision
`ar_1786505887255_bcbcc11d58bcc11b` scored 3/3 recall, 4/4 precision, and 5/5
completion. DeepSeek revision `ar_1786505887347_8f3b9d55275ef3a8` scored 2/2
recall, 3/3 precision, and 4/5 completion, with one
`result_validation_failure`. Both retained one uncertain expected claim rather
than forcing a judgment. Focused race, vet, build, and CodeScene checks pass.

### Stop rule

Do not automate semantic matching with an LLM in this slice. The first scoring
loop is deterministic except for explicit human judgment.

## Slice 19 — Experiment comparison

Status: **Implemented locally**

Depends on: Slice 18 and the finalized DEV-66 execution provenance contract.

### Goal

Compare exact Review Profile, Reviewer, model, effort, transport, and Review
Party build experiments without silently pooling incompatible case sets or
incomplete executions.

### Proposed CLI behavior

```text
review-party eval compare --baseline <run-set> --candidate <run-set>
```

The comparison reports case coverage, recall, precision, clean-case behavior,
completion, termination distribution, and runtime. It presents tradeoffs rather
than declaring a universal winner or delivery gate.

### Implementation checklist

- [x] Refine the comparison test-intent row before tests.
- [x] Define experiment identity from case revisions, effective Profile
  Revisions, Reviewer/model/effort, harness/transport, and Review Party build.
- [x] Include effective Retry Policy and Concurrency Limit in experiment
  provenance. Different Retry Policies are rejected or partitioned; different
  Concurrency Limits remain visible as runtime context.
- [x] Reject or clearly partition mismatched case revisions and missing
  adjudication.
- [x] Compare the intersection and report omitted cases; never silently change
  denominators.
- [x] Show absolute values and deltas for quality, completion, and runtime.
- [x] Keep project governance outside the comparison result.

### Smallest purposeful test set

- Two compatible run sets produce exact metric deltas and case counts.
- Different case revisions are rejected or separated visibly.
- A candidate with better recall but worse precision displays both changes.
- Infrastructure failures change completion/termination evidence but do not
  masquerade as Reviewer misses.
- Different Retry Policies cannot be presented as one direct quality
  comparison; concurrency-related runtime deltas remain explicit.

### Acceptance checklist

- [x] A real baseline/candidate Profile Revision comparison is reproducible from
  stored records and adjudications.
- [x] Every displayed aggregate can be traced to case-level evidence.
- [x] Focused comparison, ledger, and CLI tests plus CodeScene gates pass.

Acceptance evidence (2026-08-12): `eval compare` loads two immutable
Adjudication Revisions, partitions exact case-ID/digest intersections and
omissions, rescored shared case decisions deterministically, and reports
baseline/candidate values plus deltas for quality, completion, termination, and
runtime. Focused engine and CLI tests cover compatible deltas, mismatched
revisions, incomplete-result termination, JSON round-trip, and provenance.
The comparison deliberately has no universal score, promotion behavior, or
delivery gate.

### Stop rule

Do not add weighted universal scores, automatic promotion, CI delivery gates,
statistical significance claims, or model leaderboards.

## DEV-67 — Durable Eval Suite progress and lifecycle

Status: **Complete**

Depends on: Slice 19. This is the first child of DEV-66 and precedes DEV-69.

### Completed behavior

- [x] Full-suite preflight materializes every Eval Run in manifest order before
  the first Reviewer launch.
- [x] Eval Runs persist explicit Pending, Running, Completed Clean, Completed
  Findings, and Incomplete execution states plus `updated_at`, with linked
  Review IDs only after ordinary Review creation.
- [x] Eval Suite Runs persist explicit Pending, Running, Completed, and
  Incomplete lifecycle plus cancellation or hard-stop termination facts.
- [x] Initial parent/child creation and every child/parent progress checkpoint
  are atomic SQLite transactions.
- [x] Cancellation and hard checkpoint failures stop new work, retain pending
  cases, and persist the strongest honest parent state available.
- [x] Ordinary Review execution, adjudication eligibility, immutable rerun
  history, and inspection remain the existing shared paths.

Acceptance evidence (2026-08-14): focused engine, store, and CLI tests cover
preplanned manifest-order children, lifecycle categories, cancellation, hard
checkpoint failure, transaction rollback, parent-count rollback, inspection,
migration, and independent reruns. Focused race tests, `go vet`, CLI build/help,
`git diff --check`, final CodeScene file scores, and the pre-commit Code Health
safeguard pass. Isolated Muse/OpenCode dogfood on unchanged Subject
`8d8b59d0d8f5d2c71c0a50136f041b833781351c9986c184d43ec3c50b95fe6e`
completed with `canonical-v2` provenance: bugs
`rp_1786722637752_d8e8b213f77635e2` and documentation
`rp_1786722831154_4f2bd671a8a56e98` were clean; code-quality
`rp_1786722730288_11a0384ad1dd3633` completed with reviewed architectural
suggestions but no accepted in-scope runtime defect. This evidence-only ledger
closeout follows that reviewed Subject. The implementation remains uncommitted
pending explicit delivery authority.

Schema-v6 migration note: pre-DEV-67 suite rows had no explicit lifecycle.
Rows with `completed_at` migrate to Completed; rows without it migrate to
Incomplete because an abandoned historical process cannot still be Pending or
Running. This is an honest terminal normalization, not reconstructed runtime
history.

## DEV-69 — Finite retries under one Experiment Configuration

Status: **Complete**

- [x] Freeze max Attempts and finite jittered backoff in each Suite Run.
- [x] Retain retry Attempts inside one ordinary Review and Eval Run.
- [x] Retry temporary availability, transport, deadline, and malformed-result
  outcomes without Fallback or Substitution.
- [x] Persist and honor structured provider retry delays within the frozen
  maximum backoff.
- [x] Treat authentication, cancellation, configuration, corpus, and ledger
  failures as terminal.
- [x] Continue the suite after an exhausted case remains visibly Incomplete.
- [x] Reject direct comparisons whose Retry Policies differ.

Acceptance evidence (2026-08-19): focused engine and comparison tests prove
retry-then-success, bounded provider delay, terminal authentication failure,
suite deadline, exhaustion with later-case continuation, durable ordered
Attempts, and incompatible-policy rejection.

## DEV-68 — User-controlled bounded concurrency

Status: **Complete**

- [x] Default to one active Reviewer execution and accept a positive numeric
  user, Experiment, or CLI override.
- [x] Bound Reviewer execution independently per Suite Run without automatic
  resource sizing or a process-wide scheduler.
- [x] Release Reviewer capacity during retry backoff.
- [x] Preserve manifest-ordered Eval Run identity under parallel completion.
- [x] Retain existing cancellation, hard-stop, and atomic checkpoint behavior.

Acceptance evidence (2026-08-19): focused race tests prove limit-one
sequential behavior, exact bounded parallel activity, stable manifest order,
and compatibility with retry/lifecycle persistence.

## Slice 20 — Seeded controlled defects

Status: **Implemented locally**

Depends on: Slice 19, completed DEV-66 execution semantics, and evidence that
the historical/clean corpus is usable.

### Goal

Add deterministic controlled mutations when historical evidence is ambiguous,
while keeping real defects and clean cases as the core benchmark.

### Implementation checklist

- [x] Refine the seeded-case test-intent row before tests.
- [x] Represent each mutation as a reviewed, version-controlled patch with a
  stable ID, source commit, expected location, root cause, and expected behavior.
- [x] Apply the mutation only inside a temporary committed-Subject worktree.
- [x] Prove the mutation applied cleanly and changed exactly the declared files.
- [x] Record seed provenance plus the resulting Subject identity in the Eval Run
  and linked ordinary Review.
- [x] Exercise seeded cases through the same retry, concurrency, lifecycle, and
  adjudication path as historical cases; do not create a seeded-only runner.
- [x] Start with one undeniable ignored-persistence-error seed; expand only with
  separately reviewed patches.
- [x] Keep the target corpus approximately balanced over time: real historical
  defects first, seeded controlled defects second, and known-clean changes
  always present.

### Smallest purposeful test set

- Mutation setup fails before Reviewer launch if its source commit or patch no
  longer matches.
- The evaluated Subject contains the declared mutation and no source-repository
  changes remain afterward.
- A seeded case participates in the same ordinary Review and adjudication path
  as historical cases.

### Acceptance checklist

- [x] At least one seeded case runs end to end and is distinguishable from a
  historical case in results.
- [x] The seed mechanism is deterministic without live model generation.
- [x] Focused Git, case, evaluator, cleanup, race, vet, build, and CodeScene
  gates pass.

Acceptance evidence (2026-08-19): `global:seeded-bugs@seeded-bugs-v1` pins a
deterministic committed source and reviewed ignored-error patch. Real Git
worktree tests prove commit mismatch and changed-path drift stop before launch,
the authority fixture remains unchanged, seed provenance is durable, and the
ordinary Review/adjudication path receives the Git-free Synthetic Subject. A
real configured-default Muse/OpenCode run completed with Findings as Suite Run
`esr_1787168794676_6fad4a05cf53be60`, Eval Run
`er_1787168794676_506f0388fc649ebf`, and ordinary Review
`rp_1787168794687_61a1aa3b91c5d242`; inspection retained the pinned commit,
patch digest, exact path allowlist, configured Retry Policy and Concurrency
Limit, and `canonical-v2` Review provenance.

### Stop rule

Do not generate synthetic repositories, mutate production worktrees, or allow a
Reviewer to generate its own gold cases during scoring.

## Slice 21 — ACPX transport adapter

Status: **Pending**

Depends on: Slices 9-19 and completed DEV-66 execution semantics. Seeded
defects are optional.

### Goal

Add an ACP-backed harness without making ACPX semantics part of the Review Party
domain model, now with enough operational and eval evidence to compare it fairly
against direct execution.

### Implementation checklist

- [ ] Revalidate the existing ACPX research against current primary sources and
  the installed version before source changes.
- [ ] Refine a transport-specific purposeful test ledger.
- [ ] Wrap ACPX one-shot execution behind the existing capability-aware
  Attempt-execution seam.
- [ ] Support the agreed read/search-only contract, no-terminal behavior,
  cooperative timeout, hard watchdog, cancellation, and process cleanup.
- [ ] Normalize ACP events, permissions, stop reasons, usage, raw artifacts, and
  diagnostics into existing operational contracts.
- [ ] Pin or verify compatible ACPX behavior at launch because ACPX is pre-1.0.
- [ ] Reuse canonical result fixtures and eval cases rather than creating an
  ACPX-specific result path.
- [ ] Prove that ACPX implements one Attempt while retry, backoff, suite
  deadlines, cancellation, and Concurrency Limit remain Eval-engine behavior.

### Acceptance checklist

- [ ] The Conductor cannot distinguish successful direct versus ACPX execution
  except through recorded provenance.
- [ ] ACP permission mediation is not mislabeled as OS sandboxing.
- [ ] Protocol or adapter incompatibility produces a precise incomplete result
  and retained evidence.
- [ ] Focused adapter, cleanup, artifact, ledger, and canonical-result tests plus
  CodeScene gates pass.

## Slice 22 — Native-versus-ACP adapter experiments

Status: **Decision gate**

Depends on: Slice 21.

Direct OpenCode execution already exists. Native-versus-ACPX comparison and Pi
experiments remain gated on a named Profile demonstrating a concrete need.

### Implementation checklist

- [ ] Use identical committed Subjects, Eval Case revisions, Profile Revisions,
  model where possible, effort, Retry Policy, Concurrency Limit, capability
  contract, and result schema.
- [ ] Compare native OpenCode with ACPX-to-OpenCode using stored quality,
  completion, termination, timing, artifact, and cleanup evidence.
- [ ] Compare Pi RPC with ACPX-to-Pi only if a desired Profile benefits from Pi.
- [ ] Record launch-to-first-event if reliably available, wall time, selected
  model, visible tools, attempted/denied calls, stop reason, incomplete signals,
  token/cost data when authoritative, event fidelity, schema failures, and
  child-process cleanup.
- [ ] Promote only integrations that offer a named Profile a concrete advantage.

### Stop rule

Do not embed Pi SDK or build a native Go ACP host merely to remove a subprocess.
Open a dedicated design slice only when measurements demonstrate a requirement
the existing harnesses cannot meet.

## Slice 23 — Fix verification and bounded continuation

Status: **Pending**

Depends on: structured Findings, committed Subjects, replay relationships, and
the accepted domain definition of Verification Review.

### Goal

Support the relationship between an initial Review and checking fixes without
restarting an expensive broad audit or mutating the original Review.

### Implementation checklist

- [ ] Design the Verification Review Interface twice before implementation.
- [ ] Accept the prior validated outcome, caller dispositions, and exact
  remediation delta as attributable inputs to a new Review.
- [ ] Keep verification scoped to prior accepted Findings and regressions caused
  by their fixes.
- [ ] Preserve Reviewer identity when required; any change is explicit
  provenance, never substitution.
- [ ] Enforce a finite pass/Attempt budget and diagnosed transport retry rules.
- [ ] Reuse the finite retry semantics without implicitly adopting Eval Suite
  concurrency; a Verification Review remains its own ordinary Review.
- [ ] Surface residual risk rather than chasing a clean result indefinitely.

### Acceptance checklist

- [ ] A scoped Verification Review consumes only relevant prior evidence.
- [ ] The original Review and Verification Review remain independent records.
- [ ] Resolved Findings are not reopened through unchanged-code exploration.
- [ ] The workflow cannot enter an unbounded review/fix loop.

## Slice 24 — Thin skill integration and migration

Status: **Pending**

Depends on: behavior-based parity for the supported review and verification
workflow.

### Goal

Move deterministic execution out of the dotfiles skill while retaining its
human-facing policy, validation, remediation authorization, and delivery
governance responsibilities.

### Implementation checklist

- [ ] Add a Review Party invocation path without deleting the current runner.
- [ ] Compare behavior on the version-controlled corpus for clean, findings,
  incomplete, malformed, unavailable-agent, timeout, retry exhaustion,
  cancellation, bounded-concurrency, and verification cases.
- [ ] Keep local finding validation, authorization-sensitive remediation,
  project governance, and presentation in the skill.
- [ ] Switch the skill default only after parity evidence is reviewed.
- [ ] Request path-specific approval before deleting or archiving any replaced
  skill file.

### Acceptance checklist

- [ ] The skill is a short invocation and governance workflow rather than an
  executable specification.
- [ ] Supported behavior has explicit parity evidence.
- [ ] Rollback to the prior runner remains possible during migration.

## Slice 25 — Supported local delivery baseline

Status: **Pending**

Depends on: accepted scope through Slice 24. Deferred experiments do not block a
release unless explicitly made part of the supported contract.

### Goal

Establish the first supported local release and close the initial skill
migration without claiming hosted or distributed capabilities.

### Implementation checklist

- [ ] Audit every accepted domain invariant and completed slice acceptance
  criterion.
- [ ] Rehearse a fresh local install, configuration, Profile discovery, Review,
  inspect, history, committed Subject, replay, eval, adjudication, comparison,
  and supported Verification Review.
- [ ] Rehearse the sequential default plus a caller-selected higher
  Concurrency Limit, retry exhaustion, suite cancellation, and suite deadline;
  verify inspect/history expose the effective execution configuration.
- [ ] Run focused verification, final CodeScene safeguard, dependency/license
  audit, and the repository's CI gate.
- [ ] Document state layout, migration/rollback, artifact sensitivity, required
  external harnesses, diagnostics, incomplete semantics, and backup behavior.
- [ ] Create commits, tags, remote changes, and release artifacts only with
  explicit delivery authorization.
- [ ] Request path-specific deletion approval before retiring the old runner.
- [ ] Record deferred hosted execution, Parties, lineage, OTel, remote
  synchronization, and additional transports as future work.

### Acceptance checklist

- [ ] A fresh local installation can execute and diagnose every supported path.
- [ ] Documentation, CLI help, schemas, migrations, and observed behavior agree.
- [ ] Legacy JSON rollback remains available for the documented compatibility
  window.
- [ ] The old skill path is retired only with explicit approval and recovery
  evidence.

## Slice 26 — Party composition and Review Bundles

Status: **Implemented locally**

Depends on: Slices 8a, 13-15 (profiles, ledger, committed Subjects), DEV-68
bounded concurrency. User-directed out of order ahead of Slices 21-25.

### Goal

Let a Caller compose several packaged or authored Profiles over one frozen
Review Subject, execute them through the ordinary Review path with bounded
concurrency, and persist one inspectable Review Bundle that preserves each
member Record's provenance and completeness.

### Implementation checklist

- [x] Refine the party test-intent rows before tests.
- [x] Strict version-1 Party definitions in `.reviewparty/parties/` and the
  Personal library with Repository shadowing Personal shadowing packaged
  `standard`; name must match file name; duplicates, unknown fields, and empty
  member lists rejected.
- [x] Per-member reviewer/model/effort pins; explicit caller flags narrow every
  member and freeze a distinct Party Revision digest including compiled Profile
  Revision hashes.
- [x] Fail-closed preflight: one shared Subject resolution plus full member
  compilation before any Bundle row or Agent Harness launch.
- [x] Members execute through `runPreparedReview` (ordinary Reviews) with
  bounded concurrency reusing the attempt gate; sequential default.
- [x] Bundle lifecycle: pending → running → completed only when every required
  member completed; coverage incompleteness records no bundle termination while
  cancellation/hard-stop drains launched children and records a categorical
  termination.
- [x] SQLite migration 008 (`review_bundles`) plus create/save/load with exact
  round-trip; `inspect rb_...` dispatch.
- [x] CLI `parties` listing and `party run` with human and JSON output; exit 2
  for an Incomplete Bundle.
- [x] README section, this ledger entry, and the conductor-v1 dated update so
  deferred-party prose no longer misleads.

### Acceptance evidence

- Focused engine, store, and CLI tests cover fail-closed preflight, one frozen
  Subject identity across children, incomplete-bundle visibility of completed
  members, manifest order under concurrency, definition-driven concurrency
  limits, repository shadowing of packaged parties, and SQLite round-trip;
  race tests pass on all affected packages.
- CodeScene scores for new files after safeguard-driven refactoring:
  engine/party.go 10.0, engine/party_library.go 10.0, cmd/review-party/
  party.go 10.0; touched store.go and types.go remain 10.0; main.go 9.61 with
  no regression. The pre-commit Code Health safeguard passes with zero
  findings on the final change set.
- Live dogfood (2026-08-21, isolated scratch state, codex/gpt-5.6-luna/high):
  - Run 1, `--concurrency 2`: bundle rb_1787334540091_e6278fd17e616471 over the
    party implementation's own working changes completed code-quality
    (4 findings) and documentation (3 findings) while bugs honestly recorded
    deadline_exceeded; the bundle stayed inspectably Incomplete with completed
    members' findings preserved. The concurrency-selection defect exposed by
    those findings was remediated in this change set.
  - Run 2, `--concurrency 3`: bundle rb_1787335754270_9dc3d01cb0e5df93,
    party revision cb8a7a1b..., subject c63823ce...: Completed 3/3 — bugs
    (1 finding), code-quality (3), documentation (1).
- Finding dispositions for run 2: the repeated HIGH claim that file-defined
  Parties using `concurrency_limit` are rejected is false — disproven by the
  passing TestPartyDefinitionConcurrencyLimitDrivesExecution regression test
  and a live repository-party execution with that field set; the reviewer
  misread decodeStrictObject's null-guard list as a field allowlist.
  Repository-layer shadowing requiring `--repo` matches packaged Profile
  behavior. The sequential/concurrent orchestration split remains two readable
  loops sharing absorb/finalize/stop helpers rather than one conditional
  machine; no test-visible defect.

## Slice 26a — Layered party composition

Status: **Implemented locally**

Depends on: Slice 26. User-directed.

### Goal

Let a global (personal or org) baseline Party and repository-local Parties
compose into one seamless run: one command, one frozen Subject, one Review
Bundle containing the inherited baseline members plus local additions, with
recorded definition-time overrides.

### Implementation checklist

- [x] `extends`: ordered parent names on schema-1 definitions; parents resolve
  through normal shadowing; self/duplicate/invalid names rejected at parse.
- [x] Composition folds the chain into one effective definition: inherited
  members first in declared order, redefined profiles replaced at position with
  local settings, new locals appended; inherited concurrency_limit applies when
  the extending definition omits it.
- [x] Cycles and unknown parents fail closed before any launch.
- [x] `defaults.party` in Repository and Personal Configuration; Party
  selection resolves Explicit > Repository > Personal > packaged `standard`.
- [x] `parties` listing shows declared extends chains (human + JSON).
- [x] Regression tests: consolidation into one bundle, override semantics,
  cycle/missing-parent fail-closed, default-party chain and packaged fallback,
  packaged-standard extension, inherited concurrency bound. The
  concurrency-inheritance test caught a real merge defect during development
  and was fixed in the same change set.

### Acceptance evidence

Focused engine tests pass including race verification; full sweep (vet, gofmt,
focused packages) clean. Code Health: party_library.go restored to 10.0 after
splitting definition validation into focused helpers; party.go remains 10.0.

Live layered evidence (2026-08-21): an isolated Personal library provided
org-baseline (bugs + documentation); a fixture repository defined gate extending
it with a codex-pinned code-quality member plus default Party configuration.
Bare `party run --repo …` consolidated both layers into one Bundle
rb_1787338518111_9d9dfebf0f6eb065, Completed 3/3 (party revision d9f9e2aa…),
with inherited members listed before the local addition.

## Deferred beyond the active roadmap

- Finding lineage across Reviews (`first_seen`, `seen_again`, `resolved`, or
  `reintroduced`) until human adjudication produces evidence for a stable
  matching rule.
- Automated LLM semantic judging until the human adjudication corpus can measure
  the judge itself.
- OpenTelemetry, metrics backends, dashboards, distributed tracing, and hosted
  log aggregation until Review Party has hosted workers, queues, or remote
  execution whose behavior cannot be understood from durable Review Records.
- Event-sourced reconstruction; SQLite stores current facts and optional logs
  explain how they happened.
- Hosted or remote database abstraction, cross-machine synchronization, and
  multi-user authorization.
- Review Dependencies, Party-level Synthesis Reviews, hosted workers, and native
  ACP hosting until their existing domain triggers are met; basic Party
  composition now ships in Slice 26 while these extensions remain deferred.

## Immediate next action

Proceed with Slice 21, the ACPX transport adapter. DEV-63 and DEV-64 remain
separate investigation tracks and are not prerequisites unless implementation
reveals a concrete ownership collision.
