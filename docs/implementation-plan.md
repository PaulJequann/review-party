# Review Party implementation plan

Status: active planning ledger

This document orders the work from project inception through replacement of the
current skill-owned execution machinery. It records completed groundwork as
well as future slices so a new session can see the whole sequence.

The terminology in the research notes and prototype is provisional. In
particular, `ReviewRequest`, `ReviewPackage`, `lane`, and similar names are not
accepted domain language. Slice 3 exists to settle that language before types,
commands, or package boundaries are designed.

## Status legend

- **Complete locally**: work and acceptance evidence exist in the current
  worktree, but the repository has not yet made its first commit.
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

## Slice 1 — Repository foundation

Status: **Complete locally**

### Goal

Create a safe repository in which the review tool can be designed and built.

### Completed work

- Initialized `/home/pj/dev/personal/review-party` as a Git repository on local
  `main`.
- Adapted the portable agent, deletion, scratch, dependency, CodeScene,
  verification, research, and Go rules from Kashbot into [`AGENTS.md`](../AGENTS.md).
- Added a minimal [`README.md`](../README.md), documentation index, ignored
  `scratch/`, and `docs/research/` convention.

### Acceptance evidence

- Repository exists on `main` with no dependencies or implementation code.
- `scratch/` is ignored by Git.
- Scaffold whitespace checks passed.
- No initial commit or remote exists yet.

## Slice 2 — Architecture and harness research

Status: **Complete locally**

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

Status: **Next**

### Goal

Establish the user-recognized concepts, lifecycle, and ownership boundaries for
Review Party before naming types or commands.

### Operations

1. Use `domain-modeling` and `grilling` with concrete review situations.
2. Decide what begins when the user asks for a review and what durable thing, if
   any, counts as “the review.”
3. Decide what a named review profile promises: intent only, a complete
   execution strategy, or another concept.
4. Determine whether one review may contain multiple independent reviewers and
   how disagreement is represented.
5. Define user-meaningful lifecycle states, including malformed output,
   unavailable capabilities, cancellation, retry, fallback, and completion.
6. Decide whether checking fixes continues an earlier review or creates a
   related new review.
7. Map ownership among the invoking skill, Review Party, harness, transport,
   and project governance.
8. Record only confirmed language and decisions in a durable domain document.

### Non-goals

- Do not write Go code or choose Go package names.
- Do not accept the prototype vocabulary merely because it already exists.
- Do not decide configuration syntax, persistence format, or adapter interface.
- Do not assume multi-reviewer profiles, automatic fallback, or persistent
  history are required.

### Acceptance evidence

- The user can explain each accepted term in concrete review scenarios.
- The lifecycle distinguishes a valid clean outcome from incomplete execution.
- Ownership of policy, execution, and transport behavior is explicit.
- Rejected and deferred concepts are recorded alongside accepted concepts.

### Stop rule

Do not proceed to Slice 4 while core terms still mean different things in
different scenarios.

## Slice 4 — Design the executable contract twice

Status: **Implemented for filesystem-backed V1**

See [`design/profile-library-v1.md`](design/profile-library-v1.md).

### Goal

Translate the accepted domain model into the smallest executable CLI boundary
without prematurely copying the current skill implementation.

### Operations

1. Use `codebase-design` to create two materially different interface designs.
2. Compare at least:
   - a stateless command that accepts a fully resolved review description; and
   - an orchestration command that resolves user-facing targets and maintains a
     review lifecycle.
3. For each design, trace general review, specialized review, unavailable
   reviewer, malformed output, and fix-verification scenarios.
4. Decide the authoritative input, output, error, capability, snapshot identity,
   and diagnostic shapes using the accepted domain language.
5. Define what is versioned and which raw adapter artifacts remain diagnostic
   rather than public contract.
6. Select one design and capture the decision, rejected alternative, non-goals,
   and migration implications.

### Acceptance evidence

- The chosen interface is smaller than the behavior it hides.
- Callers do not need to know transport-specific event formats or launch flags.
- Clean, findings, incomplete, cancelled, and invalid-output outcomes cannot be
  confused programmatically.
- The contract supports one end-to-end vertical slice without requiring every
  future adapter or profile.

## Slice 5 — Go bootstrap and one deterministic local path

Status: **Pending**

### Goal

Create the Go module and prove the selected contract without invoking a live
coding agent.

### Operations

1. Initialize the Go module and minimal command entry point.
2. Implement only the accepted core types and one in-memory or fixture-backed
   adapter.
3. Add one built-in review behavior sufficient to compile a concrete execution
   description from a fixed subject.
4. Parse a captured successful result and a captured incomplete result into the
   public outcome contract.
5. Emit deterministic machine-readable output and useful human diagnostics.

### Verification intent

Before writing tests, use `purposeful-test-design` to identify realistic faults.
At minimum, prove that malformed or incomplete adapter output cannot become a
clean outcome and that an unsupported capability prevents launch.

### Acceptance evidence

- A focused command runs locally without network access or installed agents.
- New analyzable source files satisfy CodeScene and focused Go checks.
- No third-party dependency is added without explicit approval.

## Slice 6 — Immutable review-subject resolution

Status: **Pending**

### Goal

Resolve the first agreed user-facing Git target into an immutable, inspectable
subject for repeatable review.

### Operations

1. Implement only the first target form selected during domain modeling, likely
   the current working changes or an explicit commit.
2. Capture changed paths, diff payload, repository root, and a stable identity.
3. Detect an empty subject and target mutation before or during execution.
4. Establish size/materiality guards without silently truncating the subject.
5. Keep Git command construction argv-based and repository-scoped.

### Acceptance evidence

- The same resolved subject yields the same identity and payload.
- Untracked files follow an explicit, tested rule.
- Empty, oversized, and mutated subjects have distinct observable outcomes.

## Slice 7 — First live adapter and end-to-end review

Status: **Pending**

### Goal

Run one real review through the best-supported initial harness while preserving
the selected domain and output contracts.

### Likely starting point

Direct Copilot is the current evidence-backed candidate because its native CLI
can hide unavailable tools from the model and emits structured output. Confirm
that choice against the live installed version before implementation.

### Operations

1. Implement adapter availability and capability reporting.
2. Launch with explicit argv, working directory, model, effort, tool visibility,
   non-interactive behavior, timeout, cancellation, and process-tree cleanup.
3. Normalize native events without exposing their schema to core domain code.
4. Validate the agent's final result against the canonical outcome contract.
5. Preserve bounded diagnostics and usage telemetry without logging secrets or
   unrestricted repository contents.

### Acceptance evidence

- A real bounded review completes on an immutable subject.
- Missing binary, rejected model, timeout, denied tool, malformed output, and
  non-zero exit paths remain incomplete rather than clean.
- Fixture replay covers the adapter decoder without requiring live model calls.

## Slice 8 — Named review profiles

Status: **Pending**

### Goal

Expose the first user-recognized specialized review behaviors without
conflating intent with runtime details.

### Operations

1. Implement only the profiles accepted in Slice 3, beginning with the smallest
   useful set.
2. Give each profile explicit required capabilities, prompt material,
   materiality threshold, budget, and result contract.
3. Validate profiles before launch and explain the compiled execution in a
   dry-run or inspection command.
4. Keep agent/model/transport defaults visible and reject unauthorized
   substitution.
5. Avoid inheritance or arbitrary user scripting until repetition proves the
   need.

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

Run Slice 3 as a domain-modeling and grilling session. Start with one concrete
question: when the user says “review my current changes for bugs,” what single
thing does Review Party promise to do and return? Do not introduce a type name
until the user recognizes the underlying thing.
