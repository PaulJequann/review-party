# Configuration Hub implementation plan

Status: Slice 2 complete; remaining slices pending
Last reconciled: 2026-08-23

This plan replaces Review Party's fragmented personal configuration experience
with one recurring Configuration Hub while retaining explicit Personal and
Repository Configuration scopes. The accepted language lives in
[`../CONTEXT.md`](../CONTEXT.md).

## Outcome

An interactive terminal user runs:

```sh
review-party config
```

and receives a persistent, searchable Configuration Hub for Reviewers, models,
Profiles, Parties, repository settings, and advanced execution settings. Agents
and automation use explicit domain-oriented subcommands beneath the same
`config` command. Both interfaces call the same configuration Module and produce
the same Effective Configuration.

Personal Configuration has one canonical home:

```text
${XDG_CONFIG_HOME:-$HOME/.config}/review-party/
├── config.json
├── profiles/
│   └── <name>.md
└── parties/
    └── <name>.json
```

Repository Configuration remains team-owned under:

```text
<repository>/.reviewparty/
├── config.json
├── profiles/
│   └── <name>.md
└── parties/
    └── <name>.json
```

Managed state, Review Records, artifacts, and disposable Model Discovery cache
remain outside configuration. Review Party is pre-release: replace the old
`~/.reviewparty/` personal library directly rather than adding migration,
backfill, or legacy-read paths.

## Locked product decisions

- Cobra owns command routing, flags, argument validation, help, errors, and
  shell completion.
- Bubble Tea owns the persistent Hub and asynchronous state transitions.
- Huh owns focused forms embedded in the Hub.
- Bubbles supplies standard searchable lists, status indicators, help, and
  viewports where useful.
- Lip Gloss supplies restrained styling with readable no-color behavior.
- The Hub is recurring configuration management, not a first-run wizard.
- A non-TTY `review-party config` prints concise guidance and performs no
  configuration change.
- Human users primarily use the Hub. Agent-facing intents remain explicit,
  composable, and machine-readable.
- Hub changes are staged in memory and saved as one reviewed transaction.
  Cancellation before confirmation writes nothing.
- Effective values always expose packaged, Personal, Repository, or explicit
  provenance.
- Model Discovery queries Agent Harnesses dynamically, remains observational,
  and never authenticates implicitly.
- Searchable discovery always retains manual model entry with a warning when a
  model was not discovered.
- Review Party never stores provider credentials. Explicit sign-in actions may
  launch a harness's documented authentication flow.
- Substantive Review Profile editing uses `$EDITOR`; the Hub owns naming,
  copying, selection, validation, and post-edit summaries.
- Human help and diagnostics may improve. Existing command names, practical
  exit semantics, and machine-readable Review/Eval output contracts remain
  stable.

## Architecture

```text
Cobra commands                         Bubble Tea Configuration Hub
      │                                          │
      │ domain requests                          │ staged domain requests
      └────────────────────┬─────────────────────┘
                           ▼
                 Configuration Manager
       load · resolve · provenance · validate · plan · save
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
     Personal Configuration    Repository Configuration
              │
              └──── disposable Model Discovery cache
```

The Configuration Manager is the deep Module. Its Interface must hide JSON
shape, precedence, stable formatting, file permissions, temporary files,
multi-file publication, and rollback. Cobra, Bubble Tea, Huh, the Conductor,
and tests must not implement their own configuration rules.

Model Discovery is a separate Module behind one small interface per Reviewer.
Provider-specific commands and response formats stay in Reviewer adapters.
A discovery failure affects only that Reviewer and never prevents the Hub from
opening.

## Design boundaries twice

Slice 2 tested each material interface against a credible alternative before
freezing the pre-release contract. The comparisons below record why the
Configuration Manager exposes operations and read-only views instead of its
document and staged-state shapes.

### Effective reviewer policy

The Configuration Manager needs to answer one policy question: which effective
settings apply to each Reviewer? Two designs can answer it.

| Concern | Exported mutable field graph | Validated policy access |
| --- | --- | --- |
| Cohesion | Callers combine defaults, Reviewer policy, and provenance, and can mutate the resolved map. | `Resolve` applies precedence and validates each effective policy before read-only access. |
| Interface size and caller knowledge | The exported graph exposes the policy map and lets callers bypass its invariants. | Callers enumerate Reviewer IDs and request defensive policy values. Explicit model selection remains an execution concern. |
| Dependency direction | Execution code depends on mutable configuration storage. | Execution code depends on validated policy values; document shape and precedence remain private. |
| Testability | Tests can mutate the graph into states that `Resolve` never produced. | Table tests exercise winning values, provenance, defensive copies, and scoped errors at one seam. |
| Failure containment | Different callers can interpret disabled policy or model restrictions in different orders. | `Resolve` either returns validated policies or a scoped error. Invalid authored policy cannot escape as executable state. |
| Pre-release compatibility | Keeping the mutable graph preserves an interface no supported release requires. | Replacing it now breaks no supported users and leaves a smaller contract for later slices. |
| Measured performance | No benchmark measures direct map access. | No benchmark measures defensive policy access. The choice has no performance claim. |

Select validated policy access. The Configuration Manager resolves each policy
field by precedence before interpreting the combined policy. If the effective
`enabled` value is false, the effective model and model restriction are inert.
An error for an active Reviewer retains the scope and path of the winning value
that caused it. Explicit `--model` validation remains in profile compilation,
where the Caller request and Reviewer candidate meet.

`Effective` has no independent `Model`, and `Overrides` has no `Model` input.
`Resolve` applies per-field precedence before it validates each effective
Reviewer model and allowlist. A disabled effective policy is inert. The engine
consumes `Effective.ReviewerPolicy` and preserves the winning disabled value's
source and path.

### Staged publication

The Configuration Manager also needs to let a caller preview changes and then
publish exactly those changes. Two designs can provide that workflow.

| Concern | Exported mutable `Plan` | Opaque snapshot-bound `Plan` |
| --- | --- | --- |
| Cohesion | The plan mixes caller-facing preview fields with private staged documents. Exported slices invite callers to treat storage details as state. | The Configuration Manager owns the staged documents and snapshot identity. A separate preview value describes the proposed change. |
| Interface size and caller knowledge | Callers see validity flags, reasons, paths, scopes, changes, and reserved fields, then must know which parts publication trusts. | Callers receive an opaque plan capability and an immutable preview. They only decide whether to publish that capability. |
| Dependency direction | Publication accepts a caller-copyable value whose exported parts can diverge from its hidden state. | Callers depend on `Preview` and `Publish`; only the Configuration Manager knows the plan representation. |
| Testability | Tests can mutate exported slices, but must inspect private state to prove what publication will write. | Tests can verify that preview mutations do not affect publication, stale snapshots fail, and a fresh plan publishes the previewed change. |
| Failure containment | A long-lived plan can overwrite configuration changed after planning. Mutable preview data can misrepresent the staged write. | Publication rejects a plan when any source snapshot changed. Copied preview data cannot mutate staged state. |
| Pre-release compatibility | Retaining the struct preserves an API that no supported release requires. Reserved fields such as `Warnings` enlarge that API before semantics exist. | Replacing the struct now avoids a compatibility promise and permits later warning semantics to use a deliberate result type. |
| Measured performance | No benchmark measures copying or publishing the exported plan. | No benchmark measures snapshot comparison or defensive preview copies. The choice has no performance claim. |

Select the opaque snapshot-bound `Plan`. `Plan` exposes `Valid`, `Reason`,
`Changes`, `Scopes`, and `Paths` through read-only methods and has no `Warnings`
field. Planning captures the baseline bytes for every publication target.
`Publish` preflights all targets against those baselines and rejects a stale
plan before it writes any file. The preview methods return defensive copies, so
preview data cannot mutate the staged documents.

## Dependency order

```text
Slice 1: dependency and harness capability spike
    │
    ▼
Slice 2: scoped Configuration Manager
    │
    ├──────────────► Slice 3: Cobra command tree
    │                        │
    │                        ▼
    │               Slice 4: agent-facing config commands
    │
    └──────────────► Slice 5: Model Discovery and authentication actions
                              │
                              ▼
                     Slice 6: Configuration Hub shell
                              │
                              ▼
                     Slice 7: complete editors and atomic save
                              │
                              ▼
                     Slice 8: recovery, accessibility, and release polish
```

Slices are vertical and independently verifiable. Do not begin the visual Hub
by duplicating configuration behavior inside a Bubble Tea model.

## Slice execution protocol

For each slice:

1. Mark only that slice **In progress**.
2. Inspect shipped behavior and the named touch points.
3. Run `purposeful-test-design` immediately before adding or revising tests and
   record the slice's test-intent ledger.
4. Implement through the owning Module's Interface.
5. Run `gofmt`, focused package tests, targeted `go vet`, a targeted build,
   `git diff --check`, and the CodeScene flow required by `AGENTS.md` for every
   touched source file.
6. Exercise the named CLI acceptance scenario in isolated XDG configuration
   and state directories under `scratch/`.
7. Update this plan, README help, and durable research/design documents to match
   shipped behavior.
8. Request path-specific approval before deleting tracked obsolete files.

## Slice 1 — Dependency and harness capability spike

Status: **Complete**

### Goal

Select compatible major versions of the approved terminal libraries and prove
how each current Agent Harness can report models and initiate authentication.

### Work

- Add direct dependencies on Cobra and Huh using current compatible releases.
- Add direct Bubble Tea, Bubbles, and Lip Gloss dependencies when the spike
  imports them; avoid relying on accidental transitive imports.
- Build a scratch-only integration proving that a Huh form can participate in
  the selected Bubble Tea version without nested terminal ownership or broken
  cancellation.
- Research current official commands and structured output for Grok, OpenCode,
  Copilot, and Codex model listing and authentication.
- Record support as `supported`, `authentication_required`, `unavailable`, or
  `unsupported`; do not infer model names from documentation when the harness
  can report them.
- Define bounded deadlines, cancellation, output limits, and cache freshness for
  discovery commands.
- Save primary-source findings under `docs/research/`.

### Acceptance

- [x] Cobra `v1.10.2`, Bubble Tea `v2.0.9`, Huh `v2.0.3`, Bubbles
  `v2.2.0`, and Lip Gloss `v2.0.6` build together under Go 1.26.
- [x] The scratch spike demonstrated Hub-to-form transitions, cancellation,
  narrow-terminal resize rendering, and restoration of the caller's terminal.
- [x] Every current Reviewer has a documented discovery/authentication strategy
  or an explicit unsupported result in
  [`research/configuration-hub-dependencies-and-harness-discovery-2026-08-20.md`](research/configuration-hub-dependencies-and-harness-discovery-2026-08-20.md).
- [x] No production Hub architecture was committed from the scratch spike.

### Verification evidence

- `go mod verify`
- `go test ./cmd/review-party ./internal/engine`
- `go vet ./cmd/review-party ./internal/engine`
- `go build -o scratch/review-party ./cmd/review-party`
- `go build -o scratch/config-hub-spike/config-hub-spike ./scratch/config-hub-spike`
- Pseudo-terminal completion and Ctrl-C cancellation runs under a forced
  48-column terminal both returned control without leaving the terminal in its
  alternate screen.
- `git diff --check`

`go mod tidy` is not a repository-wide verification command here: the embedded
Eval fixture trees intentionally contain independent example module imports and
cause `./...` discovery to seek nonexistent `example.com` modules. The selected
direct dependencies and checksums are explicit in `go.mod` and `go.sum`.

## Slice 2 — Scoped Configuration Manager

Status: **Complete**

Depends on: Slice 1 dependency versions, but not the Hub spike implementation.

### Goal

Create one Configuration Manager that owns Personal Configuration, Repository
Configuration, Effective Configuration, provenance, validation, and safe
publication.

### Work

- Replace the split user-policy and global Profile-default concepts with one
  Personal Configuration model.
- Use one naming and version convention across Personal and Repository JSON.
  Scope validation may permit different fields without inventing different
  configuration languages.
- Move the personal Profile and Party library roots beneath the canonical XDG
  configuration directory.
- Remove `~/.reviewparty/`, `REVIEW_PARTY_HOME`, and old global-library behavior
  after obtaining path-specific deletion approval for any tracked files that
  must be deleted.
- Return each resolved value with provenance and distinguish absent authored
  values from effective packaged defaults. Repository reviewer settings take
  precedence over Personal settings. `Resolve` validates each effective
  Reviewer model and allowlist after per-field precedence. Disabled effective
  policies remain inert, and `Effective.ReviewerPolicy` retains the winning
  source and path for the engine.
- Define typed configuration intents rather than generic dotted JSON paths.
- Keep `Intent` as a closed set of package-owned typed operations with
  package-private mechanics. `Manager.Plan` accepts `[]Intent` and represents a
  nil intent as an invalid opaque `Plan` with read-only `Valid` and `Reason`
  methods.
- Expose semantic changes, affected scopes, and affected paths through the
  read-only `Plan.Changes`, `Plan.Scopes`, and `Plan.Paths` methods. These
  methods return copies and cannot mutate staged state.
- Publish a confirmed `Plan` atomically with private personal-file permissions
  through `Manager.Publish`.
  Multi-file failure must restore the pre-save state or leave an explicit,
  recoverable failure without claiming success.
  Planning captures baseline bytes, and publication preflights every target.
  A stale target rejects the complete plan before any write begins.
- Produce stable, readable JSON with two-space indentation, trailing newline,
  semantic field ordering, expanded nested objects, and omitted redundant
  defaults.
- Preserve packaged Profiles and Parties as immutable defaults that users can
  copy into a chosen scope before customization.
- Update initialization and the Conductor to consume this Module rather than
  raw configuration paths or maps.

### Acceptance

- One load returns effective values and exact provenance across packaged,
  Personal, Repository, and explicit Reviewer or Profile choices.
- A failed multi-file save does not expose a partially accepted configuration.
- A stale plan writes no file.
- Preview data cannot mutate the plan's staged state.
- Opening or resolving defaults does not create a file.
- Personal Profiles and Parties resolve only from the XDG configuration home.
- No migration, backfill, legacy reader, or compatibility branch remains.

### Verification evidence

- `go test ./internal/configuration ./internal/engine ./cmd/review-party`
- `go vet ./internal/configuration ./internal/engine ./cmd/review-party`
- `go build -o scratch/slice2/rp ./cmd/review-party`
- `git diff --check`
- Isolated XDG CLI exercise covered canonical paths, zero-write reads, typed
  advanced-state publication with `0700`/`0600` permissions, repository
  precedence, and malformed-document fail-closed behavior.
- Configuration Manager tests cover provenance, explicit choices, atomic
  rollback, scope validation, stable JSON, typed-plan validation including nil
  intent refusal, and private personal publication.

## Slice 3 — Cobra command tree

Status: **Pending**

Depends on: the completed Slice 2 Interface.

### Goal

Replace manual dispatch and repeated standard-library `FlagSet` construction
with a coherent Cobra tree without changing Review Party's execution semantics.

### Work

- Keep `main` limited to signal context, root-command construction, execution,
  and exit-code mapping.
- Build commands through constructors with injected input, output, error output,
  environment, and owning Modules.
- Group commands by user task and provide concise `Use`, `Short`, `Long`, and
  `Example` text.
- Preserve current command names and flags unless an existing behavior is
  provably unusable.
- Preserve JSON output schemas and keep styled human output out of machine
  formats.
- Add shell completion, including dynamic completion for Profile, Party, and
  Reviewer names where it does not launch a harness.
- Make plain `review-party` print concise help successfully and point to
  `review-party config`.
- Configure Cobra to avoid duplicate error/usage output and map usage errors to
  the established exit code.

### Acceptance

- Existing focused CLI tests pass against Cobra-backed commands.
- Root and nested help are readable and task-oriented.
- Unknown commands receive useful suggestions.
- Review, inspect, history, Eval, Profile, and Party JSON outputs remain
  contract-compatible.

## Slice 4 — Agent-facing configuration commands

Status: **Pending**

Depends on: Slices 2-3.

### Goal

Expose the same configuration capabilities as explicit commands for agents,
automation, and noninteractive recovery.

### Initial command families

```text
review-party config show [--effective] [--scope personal|repository] [--format human|json]
review-party config validate [--scope personal|repository] [--format human|json]
review-party config reviewer enable REVIEWER
review-party config reviewer disable REVIEWER
review-party config reviewer set-default REVIEWER
review-party config reviewer set-model REVIEWER MODEL
review-party config reviewer restrict-models REVIEWER MODEL...
review-party config profile set-default PROFILE [--scope ...]
review-party config party ...
review-party config advanced ...
```

Exact verbs should remain domain-oriented and discoverable through Cobra help;
do not expose arbitrary dotted-key intents.

### Work

- Show a semantic before/after plan and require confirmation for intents.
- Support `--yes` for explicitly authorized automation.
- Return structured changed scope, affected paths, before and after values, and
  validation results in JSON mode.
- Keep stdout machine-clean; prompts and diagnostics use the correct terminal or
  error stream.
- Refuse interactive confirmation without a controlling terminal unless
  `--yes` is present.

### Acceptance

- Every Hub intent planned for Slice 7 has a typed noninteractive operation.
- Commands and the eventual Hub produce equivalent Plans.
- Invalid changes write nothing.
- Agent callers never need to parse styled terminal output.

## Slice 5 — Model Discovery and authentication actions

Status: **Pending**

Depends on: Slice 1 research and Slice 2 configuration types.

### Goal

Offer searchable, provider-correct model choices without making discovery or
authentication a prerequisite for configuration.

### Work

- Define a bounded discovery result with Reviewer, observed time, status,
  canonical model ID, optional display metadata, and diagnostics.
- Implement provider-specific discovery behind Reviewer adapters using argv,
  bounded output, cancellation, deadlines, and existing environment policy.
- Store successful results in a disposable cache under the appropriate cache
  home, never in authored configuration.
- Open consumers immediately with cached, configured, and packaged choices.
- Refresh on demand and when a Reviewer screen first opens; deduplicate
  concurrent refreshes for the same Reviewer.
- Search model ID and trustworthy harness-reported display metadata.
- Retain manual entry. Warn and request confirmation when discovery did not
  report the entered model.
- Detect authentication-required outcomes without automatically launching a
  browser or mutating harness configuration.
- Add an explicit sign-in action only where Slice 1 found a safe documented
  command; otherwise show instructions.

### Acceptance

- One slow or broken harness never blocks the Hub or another Reviewer's models.
- Cache failure degrades to configured, packaged, and manual choices.
- Discovery never changes Personal or Repository Configuration.
- Authentication starts only after an explicit user action.

## Slice 6 — Configuration Hub shell

Status: **Pending**

Depends on: Slices 2, 3, and 5.

### Goal

Make `review-party config` a responsive persistent control center with correct
terminal and nonterminal behavior.

### Work

- Detect interactive terminal capability without treating redirected output as
  a TUI.
- In a TTY, open a Bubble Tea Hub showing Personal and current Repository
  Configuration, provenance, dirty state, and discovery status.
- Outside a TTY, print concise config guidance and perform no writes.
- Establish navigation categories: Overview, Reviewers, Profiles, Parties,
  Repository, Advanced, and Review Changes.
- Use Bubbles for searchable lists, help, status, and viewports where they
  reduce custom state.
- Use restrained Lip Gloss styles that adapt to terminal color capability and
  honor `NO_COLOR`.
- Enable Huh's accessible mode through a documented environment variable and
  explicit flag; accessible mode uses standard prompts rather than redraws.
- Keep all edits in a draft that can be discarded safely.

### Acceptance

- Open, navigate, resize, cancel, suspend/resume where supported, and exit
  without terminal corruption.
- The Hub opens before live discovery finishes.
- Every displayed effective value identifies its provenance.
- Opening and exiting without saving changes no files.
- Non-TTY and accessible paths remain fully usable.

## Slice 7 — Complete Hub editors and atomic save

Status: **Pending**

Depends on: Slices 4-6.

### Goal

Complete recurring configuration management across all accepted Hub areas.

### Work

- Reviewer editor: enablement, default, effort, discovered searchable model,
  manual model, and advanced model restriction.
- Profile editor: list/explain, copy packaged, create blank, rename where safe,
  choose default, launch `$EDITOR`, validate, summarize, and remove.
- Party editor: list/explain, create, compose ordered members, configure member
  Reviewer/model/effort, choose concurrency, and remove.
- Repository editor: make scope and tracked paths unmistakable and expose the
  repository values that may override Personal Configuration.
- Advanced editor: Eval retry/backoff/concurrency and managed-state location.
- Review Changes: group semantic changes by scope and path, display validation
  results, and confirm one atomic save.
- Require ordinary confirmation for setting removal and typed-name confirmation
  before deleting authored Profile or Party files.
- Preserve completed editor changes in the draft when another editor is
  cancelled; exiting the Hub still offers discard or return.

### Acceptance

- A user can complete every currently supported configuration change without
  hand-editing JSON.
- An agent can perform the equivalent change through Slice 4 commands.
- `$EDITOR` failure or invalid Profile content returns to the Hub without
  accepting the invalid file.
- Save either publishes the complete reviewed Plan or reports failure
  without partial accepted state.
- Repository intents identify tracked-file effects before confirmation.

## Slice 8 — Recovery, diagnostics, and release polish

Status: **Pending**

Depends on: Slice 7.

### Goal

Make configuration failures recoverable and the completed experience ready to
replace manual JSON editing in documentation.

### Work

- Open a recovery screen when configuration is malformed or semantically
  invalid.
- Show the precise error, scope, path, and safe guided repairs where the
  original intent is unambiguous.
- Offer read-only inspection and export/backup before destructive recovery.
  Never silently discard malformed or unknown content.
- Add `review-party doctor` checks for configuration validity, Reviewer
  executables, authentication status, Model Discovery support, selected-model
  confidence, repository scope, and writable configuration/cache locations.
- Provide equivalent structured doctor output for agents.
- Add concise success summaries and next actions after save.
- Replace README instructions that require ordinary users to author JSON with
  Hub-first guidance; retain JSON documentation as an advanced and machine
  contract reference.
- Dogfood `bugs`, `code-quality`, and `documentation` Profiles against the same
  unchanged Subject using the required isolated OpenCode Muse path.

### Acceptance

- A malformed file no longer locks the user out of configuration management.
- Doctor distinguishes required failures, optional unavailable tools, and
  unsupported discovery without collapsing them into one error.
- Human documentation can reach a working configuration without teaching JSON
  structure first.
- Focused tests, formatter, vet, build, diff checks, CodeScene gates, and the
  required bounded dogfood Reviews complete or are reported honestly as
  Incomplete.

## Test-intent ledger

Each slice must refine this ledger through `purposeful-test-design` before tests
are written.

| Behavior | Plausible harmful defect | Boundary | Required observation |
| --- | --- | --- | --- |
| Effective Configuration preserves provenance | A repository override appears personal and the user edits the wrong scope | Configuration Manager | Every resolved value identifies source and scope |
| A confirmed draft publishes atomically | One Profile file changes while `config.json` remains old | Real filesystem publication | Forced failure restores or preserves the complete prior configuration |
| Defaults remain zero-write | Opening the Hub creates a config that later masks packaged updates | Public Hub over isolated XDG dirs | Open and exit leaves no files |
| Cobra preserves automation | Framework migration changes JSON shape or usage exit codes | Public CLI | Golden semantic JSON and exit behavior remain stable |
| Discovery is bounded and isolated | One hanging harness freezes all Reviewer configuration | Discovery Module and Hub update loop | Other screens remain responsive and cancellation reaps the process |
| Discovery remains observational | Refresh launches login or writes another tool's settings | Scripted harness fixture | No auth or configuration change occurs before explicit action |
| Manual model entry remains available | Incomplete discovery prevents selection of a valid new model | Reviewer editor | Warning can be confirmed and selected ID is saved |
| Hub cancellation is clean | Leaving a nested form writes a partially edited policy | Hub and Configuration Manager | Cancel/exit before final confirmation changes no files |
| Destructive actions preserve intent | A misfocused key deletes authored Profile content | Hub deletion flow | Typed identity and final Plan are required |
| Recovery preserves malformed input | Guided repair silently drops an unrecognized field | Recovery flow over real files | Original bytes remain available until explicit confirmed replacement |
| Machine commands do not require a TTY | An agent blocks waiting for Huh confirmation | Cobra config command | Non-TTY configuration change refuses unless explicit authorization is supplied |
| Profile editor validates external edits | `$EDITOR` writes an invalid Profile that becomes effective | Editor integration and Profile compiler | Invalid content is rejected before publication |

## Explicit non-goals

- No web configuration UI.
- No Viper dependency or generic environment-to-field binding layer.
- No arbitrary dotted-key setter.
- No credential storage.
- No Review Party-maintained universal model catalog.
- No automatic browser login during Model Discovery.
- No migration or compatibility layer for `~/.reviewparty/`.
- No full-screen TUI for ordinary Review JSON output merely for visual novelty.
- No weakening of fail-closed Reviewer, model, capability, or incomplete-result
  semantics.

## Expected obsolete behavior

Implementation is expected to make the following concepts obsolete:

- the separate `~/.reviewparty/` global library;
- `REVIEW_PARTY_HOME` as a personal-library selector;
- separate user-policy and global Profile-default configuration models;
- manual root command dispatch and repeated standard-library FlagSets;
- `review-party config` as only `path|show`;
- README-first manual JSON configuration.

Before deleting any tracked source or documentation path, list the exact paths
and request approval as required by `AGENTS.md`. Remove obsolete behavior once
approved rather than retaining compatibility branches.
