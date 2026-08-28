# Configuration Hub implementation plan

<!-- Stashbox: https://stashbox.local.bysliek.com/0XZZMwVbSdKL -->

Status: Slices 1-6 complete; Slice 7 is next
Last reconciled: 2026-08-27

This plan replaces Review Party's current configuration and execution model with
the accepted Global and Repository Configuration model in [`../CONTEXT.md`](../CONTEXT.md).
It incorporates the Configuration Manager and Cobra work delivered in PRs #8
and #10, then directly replaces the Party inheritance behavior delivered in PR
#9. Review Party is pre-release. Do not add compatibility readers, migrations,
or aliases for the superseded model.

## Product outcome

Humans primarily configure Review Party through the recurring terminal Hub:

```sh
review-party config
```

Agents and automation use explicit machine-readable commands under `config`.
Both interfaces call the same Configuration Manager operations.

An ordinary run uses the repository's saved review selection:

```sh
review-party run
```

An explicit selection replaces the repository default for one run:

```sh
review-party run --profile code-quality
review-party run --party baseline
```

Bare `review-party` continues to print help. It must never launch paid external
work by surprise.

## Configuration scopes

Global Configuration is available to every repository but enables nothing by
itself:

```text
${XDG_CONFIG_HOME:-$HOME/.config}/review-party/
├── config.json
├── profiles/
│   └── <name>/
│       ├── profile.json
│       └── instructions.md
└── parties/
    └── <name>.json
```

Repository Configuration owns that repository's executable default selection:

```text
<repository>/.reviewparty/
├── config.json
├── profiles/
│   └── <name>/
│       ├── profile.json
│       └── instructions.md
└── parties/
    └── <name>.json
```

Managed state, Review Records, artifacts, and disposable Model Discovery cache
remain outside configuration. Packaged Review Profile Templates are immutable
binary content, not executable Global or Repository Profiles.

## Locked product decisions

### Review Profiles

- A Review Profile is a complete executable package: judgment instructions,
  Reviewer, model, reasoning effort, and Attempt deadline.
- A Profile runs exactly as saved. Ordinary Reviews do not override its Reviewer,
  model, effort, deadline, or instructions.
- Different cost and quality choices are separately named Profiles, such as
  `code-quality-small`, `code-quality`, and `code-quality-large`. Do not add a
  variants system in this plan.
- Eval benchmarking remains the place to test alternative execution choices.
  A useful result can then be saved as a Profile.
- Profiles use a directory containing strict `profile.json` metadata and
  editable `instructions.md`. The Configuration Manager treats both files as
  one publication unit.
- Global and Repository Profiles may share a name. An unqualified explicit
  reference resolves Repository before Global. Qualified references remain
  available for exact selection.
- Global Profile references are live. Editing one affects future runs in every
  repository that selects it. Existing Review Records retain their exact
  Profile Revisions.
- V1 does not rename Profiles or Parties. Copy under a new name instead. This
  avoids breaking repositories and autonomous workflows that hold live
  references.

### Review Profile Templates

- Packaged `bugs`, `code-quality`, and `documentation` content becomes
  non-executable Review Profile Templates.
- A Template may seed Profile instructions but never supplies a guessed
  Reviewer, model, effort, or deadline.
- Profile Creation may start from a Template or blank instructions. Saving
  copies the instructions; Profiles do not inherit later Template changes.
- Each created Profile records its source Template ID and revision when one was
  used.
- The Hub and `doctor --format json` report Template drift without blocking an
  otherwise compatible Review.
- Applying a Template update is explicit. It replaces `instructions.md`, keeps
  the Profile's execution settings, warns about overwritten customizations, and
  creates a new Profile Revision.
- Only a result-contract or capability incompatibility may require a Profile
  update. A newer Template alone never blocks execution.

### Parties and repository roll-up

- A Party is a named ordered group of Profile references with one Concurrency
  Limit. Naming is a convenience for reuse, not the basis of configuration
  layering.
- Global Parties contain Global Profile references only. Repository Parties may
  contain Global and Repository Profile references.
- Parties contain no other Parties and no per-member Reviewer, model, effort,
  deadline, or instruction overrides.
- Repository Configuration owns its complete default selection. It may select
  Global or Repository Profiles and Parties.
- Global selections expand first, followed by Repository selections. Array and
  Party member order is preserved.
- Exact scoped Profile identities are deduplicated after Party expansion. The
  first occurrence keeps its position.
- `global:code-quality` and `repository:code-quality` are different Profiles and
  both run. Hub previews and CLI responses must strongly warn when same-named
  Profiles from different scopes will both execute.
- A missing Profile or Party, invalid Profile, unavailable saved Reviewer, or
  rejected model fails the entire preflight before any Reviewer launches.
- A named Party owns its Concurrency Limit. The repository default selection
  owns a separate limit for its complete roll-up.
- Review Bundles preserve both the authored selection and the expanded,
  deduplicated execution list with scoped identities and Profile Revisions.

Repository configuration uses an explicit scope-grouped selection:

```json
{
  "schema_version": 1,
  "reviews": {
    "concurrency_limit": 3,
    "global": [
      {"party": "baseline"},
      {"profile": "documentation"}
    ],
    "repository": [
      {"profile": "supabase-rls"}
    ]
  }
}
```

Global availability does not imply execution. A repository must select every
Profile or Party it wants in its default run.

### Hub and command behavior

- Cobra owns routing, typed flags, help, errors, and completion. Completion may
  read local and packaged names but never launches an Agent Harness.
- Bubble Tea owns the persistent Hub and asynchronous state transitions. Huh,
  Bubbles, and Lip Gloss provide focused forms, standard components, and
  restrained styling.
- Human users primarily use the Hub. Agents primarily use explicit config
  commands with clean JSON output.
- Every Hub operation has an equivalent typed command before that editor is
  considered complete.
- `config show` reports Effective Configuration: what will run, its ordered
  expansion, warnings, and provenance.
- `config file show [--scope global|repository]` reports authored storage for
  advanced inspection; the scope defaults to `global`. `config path` remains a
  path convenience.
- Hub changes remain staged until one reviewed atomic publication. Cancellation
  before confirmation writes nothing.
- Human mutations prompt for confirmation in a TTY and require `--yes`
  otherwise. JSON mutations always require `--yes` and never prompt.
- Accessible mode uses the same draft and intent state machine through ordinary
  prompts rather than terminal redraws.
- Model Discovery is observational and bounded. Manual undiscovered model IDs
  remain available after an explicit warning.
- Saving validates structural compatibility and discovered choices. A
  temporarily unavailable harness may be saved after warning, but execution
  still fails closed if it remains unavailable.
- Review Party never stores provider credentials or authenticates implicitly.

### Recovery and pre-release replacement

- PR #11 already replaced the incremental SQL migration chain with one initial
  schema. Retired state is not imported or upgraded.
- Recovery backs up an incompatible ledger and its SQLite sidecars under a
  dedicated state-root backup directory.
- Backup and fresh initialization require separate confirmations. Review Party
  never silently deletes, converts, or reinterprets retired state.
- Profile and Party deletion remains deferred until backup/export recovery is
  available.
- The accepted model directly removes Party `extends`, nested Party composition,
  inherited concurrency, per-member execution pins, executable packaged
  Profiles, and the old `review` and `party run` command model.

## Architecture

```text
Cobra config commands                    Bubble Tea Configuration Hub
         │                                           │
         │ typed operations                          │ staged operations
         └──────────────────┬────────────────────────┘
                            ▼
                  Configuration Manager
      load · resolve · provenance · validate · plan · publish
                            │
             ┌──────────────┴──────────────┐
             ▼                             ▼
     Global Configuration        Repository Configuration
             │                             │
             └──────────┬──────────────────┘
                        ▼
           Effective Review Selection
       expand · deduplicate · warn · preflight
                        │
                        ▼
                 Review Pipeline
```

The Configuration Manager hides document shape, path rules, precedence, stable
formatting, file permissions, temporary files, multi-file publication, stale
plans, and rollback. The Hub and commands must not duplicate those rules.

Model Discovery remains a separate Module behind Reviewer-specific adapters. A
failure for one Reviewer cannot prevent configuration of another Reviewer or
opening the Hub.

## Dependency order

```text
Slice 1  Dependency and harness capability spike                 Complete
Slice 2  Scoped Configuration Manager                            Complete
Slice 3  Cobra command tree                                      Complete
Slice 4  Domain and storage reset                                Complete
Slice 5  Resolution and review-party run                         Complete (PR #13)
Slice 6  Agent-facing configuration commands                Complete
Slice 7  Discovery and onboarding
Slice 8  Hub shell and core editors
Slice 9  Template updates, recovery, and release polish
```

Do not build Hub views against the superseded Profile or Party model. Slice 4
must replace that foundation first.

## Slice execution protocol

For each slice:

1. Mark only that slice **In progress**.
2. Inspect shipped behavior and the named ownership boundaries.
3. Run `purposeful-test-design` immediately before adding or revising tests and
   record the test-intent ledger for the slice.
4. Implement through the owning Module's interface.
5. Run `gofmt`, focused tests, targeted `go vet`, a targeted build,
   `git diff --check`, and the CodeScene flow required by `AGENTS.md` for each
   touched source file.
6. Exercise the named CLI scenario in isolated XDG configuration and state
   directories under `scratch/`.
7. Update this plan, README help, `CONTEXT.md`, and affected design documents to
   match shipped behavior.
8. Request path-specific approval before deleting tracked files.

## Slice 1: dependency and harness capability spike

Status: **Complete**

Selected Cobra `v1.10.2`, Bubble Tea `v2.0.9`, Huh `v2.0.3`, Bubbles `v2.2.0`,
and Lip Gloss `v2.0.6` under Go 1.26. The scratch integration proved embedded
form transitions, cancellation, narrow resizing, and terminal restoration.
Reviewer discovery and authentication findings live in
[`research/configuration-hub-dependencies-and-harness-discovery-2026-08-20.md`](research/configuration-hub-dependencies-and-harness-discovery-2026-08-20.md).

## Slice 2: scoped Configuration Manager

Status: **Complete; terminology and document shape change in Slice 4**

PR #8 delivered strict loading, provenance-aware resolution, typed intents,
opaque snapshot-bound Plans, stale-plan rejection, atomic multi-file
publication, private permissions, and zero-write defaults. Keep these deep
Module properties. Slice 4 renames the Global scope and replaces Profile and
Party document shapes without restoring engine-owned configuration rules.

## Slice 3: Cobra command tree

Status: **Complete**

PR #10 delivered one Cobra-owned tree, typed flag parsing, successful root help,
nested help, suggestions, shell completion, and local completion for Reviewer,
Profile, and Party names. Preserve machine-readable Review and Eval contracts.
The new `run` and configuration commands must use the same constructors and
single-parse boundary.

## Slice 4: domain and storage reset

Status: **Complete**

### Goal

Make the Configuration Manager represent the accepted Profile, Template, Party,
and repository roll-up model before adding more interfaces.

### Work

- Rename Personal Configuration to Global Configuration in domain language,
  APIs, help, and documentation. Keep the canonical XDG path.
- Add non-executable packaged Review Profile Templates with stable IDs and
  revisions.
- Replace executable packaged Profiles with Profile Creation from Template or
  blank instructions.
- Replace `<name>.md` Profiles with `<name>/profile.json` plus
  `<name>/instructions.md` and publish each aggregate atomically.
- Require Reviewer, model, reasoning effort, and Attempt deadline in every
  executable Profile.
- Replace Party members with scoped Profile references only.
- Remove `extends`, Party nesting, inherited concurrency, and member execution
  pins.
- Add the Repository `reviews` selection with Global and Repository arrays and
  one Concurrency Limit.
- Add typed intents for Profile Creation/copy, Party creation, selection editing,
  ordering, and default publication.
- Preserve source scope and Template provenance in read-only resolved values.
- Update `CONTEXT.md`, Profile/Party design documents, and JSON examples.

### Acceptance

- No packaged Template can execute directly.
- A saved Profile cannot omit any required execution choice.
- Global availability causes no Review to run without Repository selection.
- Global Parties cannot reference Repository Profiles.
- Parties cannot contain Parties or execution overrides.
- Opening and resolving defaults remains zero-write.
- No reader or migration for the superseded Profile, Party, or Personal naming
  model remains.

## Slice 5: resolution and `review-party run`

Status: **Complete (PR #13)**

### Goal

Resolve one repository selection into an inspectable Review Pipeline and execute
it through the ordinary Review path.

### Work

- Add `review-party run` with mutually exclusive `--profile` and `--party`.
- Without either flag, load the repository's saved selection.
- Replace `review` and `party run` directly; do not add aliases.
- Resolve unqualified explicit names Repository before Global and support exact
  qualified names.
- Expand Global selections before Repository selections while preserving file
  and member order.
- Deduplicate exact scoped Profile identities at first occurrence.
- Warn, in human and JSON output, when same-named cross-scope Profiles remain.
- Preflight every reference and complete Profile before Subject resolution or
  Reviewer launch.
- Use the repository selection or explicit Party Concurrency Limit.
- Persist authored selection, expanded list, deduplication facts, warnings,
  Profile Revisions, and provenance in the Review Bundle.
- Keep bare `review-party` as successful help.

### Shipped behavior notes

- Resolution lives in the Configuration Manager as `ResolveRun`; the engine only
  compiles resolved slots and authors the Bundle. A missing reference surfaces
  with its selection origin (`reviews.global[1]`) and available names.
- Review Bundles gained a `selection` record (kind, source, limit provenance,
  authored items), `warnings`, `deduplicated` facts, per-member origins and
  Profile Revisions, and one composition `revision` digest. The ledger schema
  moved to version 10; preparation replaces prior pre-release ledgers in place
  per the established no-migration policy, including earlier single-schema
  versions such as 9.
- An interactive unconfigured run currently refuses with guidance to
  initialize and configure; opening the Hub belongs to Slice 8, so both modes
  refuse without writes or launches this slice.
- Sequential execution preserves authored order; Concurrency Limits above one
  gate attempts through the existing bounded launch mechanism while bundle rows
  stay ordered by authored position.

### Acceptance

- A configured repository runs its complete saved roll-up with one command.
- Explicit Profile or Party selection replaces the default.
- Missing, invalid, or unavailable Profiles launch no Reviewer.
- Exact overlaps run once without making ordinary configuration feel broken.
- Same-named Global and Repository Profiles both run with a strong warning.
- Both interactive and noninteractive unconfigured runs refuse with guidance
  and launch no Reviewer. Hub opening is deferred to Slice 8.

## Slice 6: agent-facing configuration commands

Status: **Complete**

### Goal

Expose every accepted configuration operation to agents and automation through
explicit, machine-readable commands.

### Command families

These commands are the Slice 6 interface. They are not part of the Slice 4 CLI.

```text
review-party config show [--repo PATH] [--format human|json]
review-party config file show [--scope global|repository] [--repo PATH] [--config PATH] [--format human|json]
review-party config validate [--scope global|repository] [--repo PATH] [--config PATH] [--format human|json]
review-party config profile create NAME (--template TEMPLATE|--blank) --reviewer ID --model ID --effort EFFORT --deadline DURATION [--instructions TEXT|--instructions-file PATH] [--scope global|repository] [--repo PATH] [--format human|json] [--yes]
review-party config profile copy NAME --target-scope global|repository [--repo PATH] [--format human|json] [--yes]
review-party config party create NAME --profile PROFILE [--profile PROFILE ...] --concurrency-limit N [--scope global|repository] [--description TEXT] [--repo PATH] [--format human|json] [--yes]
review-party config reviews add --scope global|repository (--profile NAME|--party NAME) [--repo PATH] [--format human|json] [--yes]
review-party config reviews remove --scope global|repository (--index N|--profile NAME|--party NAME) [--repo PATH] [--format human|json] [--yes]
review-party config reviews move --scope global|repository --from N --to N [--repo PATH] [--format human|json] [--yes]
review-party config reviews set-concurrency N [--repo PATH] [--format human|json] [--yes]
```

The shipped `remove` command accepts an index or one Profile or Party selector.
The shipped `move` command accepts `--scope`, `--from`, and `--to`. The
`set-concurrency` value is its positional `N` argument. These commands express
domain operations, not dotted JSON paths. Repository-scoped file inspection and
repository-scoped mutations require `--repo PATH`.

Repository-targeted validation also requires `--repo PATH`; without it, the
command validates the current working directory's repository scope. Party
creation accepts repeatable `--profile` flags, preserving the authored member
order. For `config reviews add` and `config reviews remove`, a qualified
`--profile` or `--party` reference must match `--scope`: `global:NAME` with
`--scope global` or `repository:NAME` with `--scope repository`. Unqualified
references use the selected scope.

### Work

- Make `config show` report Effective Configuration, expanded Reviews,
  deduplication, warnings, and provenance.
- Keep authored storage under the explicit `config file` family.
- Return semantic before/after Plans in JSON.
- Require confirmation for mutations and `--yes` for authorized automation.
- Keep stdout machine-clean and refuse non-TTY confirmation without `--yes`.
- Implement commands and later Hub editors as vertical pairs over the same
  typed operation.

### Shipped behavior

- `config show` resolves Effective Configuration for the selected repository.
  JSON includes effective value provenance, the ordered expanded Review list,
  exact deduplication facts, same-name warnings, and the selection limit
  provenance. An unconfigured repository exits successfully and reports the
  missing selection in `selection_error`.
- `config file show --scope ...` prints one validated authored document. It
  does not report the effective result. An absent document returns its path and
  `present: false` in JSON.
- `config validate` validates one scope or both scopes. It checks authored
  Profile and Party definitions and the repository selection references without
  creating missing files.
- Profile creation, Profile copy, Party creation, and selection edits all use
  Configuration Manager Plans. JSON mutation output contains semantic changes
  with before and after values, affected scopes, paths, and publication state.
- Mutations with `--format json` require `--yes`. Human mutations require a
  terminal confirmation, and non-TTY callers must pass `--yes`.

### Acceptance

- Agents never parse styled terminal output or edit JSON directly.
- Invalid or stale Plans write nothing.
- Every operation planned for the Hub has an equivalent command before its
  editor is complete.

## Slice 7: discovery and onboarding

Status: **Pending**

### Goal

Create the first complete executable Global Profile without guessing installed
Reviewers, accessible models, effort, or cost tolerance.

### Work

- Implement bounded Reviewer-specific Model Discovery and disposable caching.
- Open consumers from cached, configured, and packaged choices before refresh
  completes.
- Keep manual model entry with a warning and explicit confirmation.
- Detect authentication requirements without implicit login; expose explicit
  documented sign-in actions only.
- Guide interactive onboarding through Template or blank instructions, Profile
  name, Reviewer, model, effort, deadline, validation, and reviewed save.
- Let noninteractive initialization prepare managed state without inventing a
  Profile.

### Acceptance

- Interactive onboarding ends with at least one validated executable Global
  Profile.
- A slow or broken harness cannot block other Reviewers or the Hub.
- Discovery never changes configuration or launches authentication implicitly.
- `review-party run` refuses clearly when no executable repository selection
  exists.

## Slice 8: Hub shell and core editors

Status: **Pending**

### Goal

Provide recurring human configuration management without exposing storage
mechanics.

### Work

- Open the Bubble Tea Hub in a TTY; print guidance without writes outside a TTY.
- Show Global and current Repository scope explicitly.
- Provide Overview, Profiles, Parties, Repository Reviews, Advanced, and Review
  Changes areas.
- Search Templates, Global Profiles and Parties, and Repository Profiles and
  Parties with visible source labels.
- Create Profiles from Template or blank instructions; use `$EDITOR` for
  substantive instruction editing.
- Create flat Parties from scoped Profile references.
- Assemble ordered Repository selections from Global and Repository Profiles or
  Parties.
- Preview expansion, deduplication, same-name warnings, provenance, and the
  complete atomic Plan.
- Support copying a Repository Profile to Global Configuration.
- Preserve draft edits across cancelled focused forms. Exiting still offers
  discard or return.
- Provide accessible prompts over the same draft state machine.

### Acceptance

- A human can create complete Profiles, Parties, and a repository default
  without editing JSON.
- Opening, navigating, and exiting without save changes no files.
- Every displayed effective value and selected item identifies its scope.
- Cancellation, resizing, and save failure leave the terminal and authored
  configuration intact.

## Slice 9: Template updates, recovery, and release polish

Status: **Pending**

### Goal

Make drift and incompatible state recoverable before adding destructive
configuration operations.

### Work

- Report Template revision drift in the Hub and `doctor --format json`.
- Show instruction diffs and explicitly replace `instructions.md` while keeping
  Profile execution settings.
- Warn before overwriting customized instructions and create a new Profile
  Revision.
- Back up incompatible ledgers and SQLite sidecars under a dedicated backup
  directory.
- Require separate confirmation before fresh initialization.
- Add Profile and Party deletion only after export/backup recovery exists; use
  typed-name confirmation and reviewed Plans.
- Replace README current-CLI documentation only as each behavior ships.
- Dogfood `bugs`, `code-quality`, and `documentation` purposes against one
  unchanged Subject through configured executable Profiles.

### Acceptance

- Template drift is visible but does not block compatible Reviews.
- Updating from a Template never changes Reviewer, model, effort, or deadline.
- Recovery preserves incompatible bytes and never claims migration success.
- Human docs reach a working repository selection without teaching JSON first.

## Test-intent ledger

Each slice must refine this ledger through `purposeful-test-design` before tests
are written.

| Behavior | Plausible harmful defect | Boundary | Required observation |
| --- | --- | --- | --- |
| Profile publication is atomic | Metadata selects a model while instructions remain from another revision | Real Configuration Manager filesystem publication | Readers observe the complete old or complete new Profile |
| Templates cannot execute | A packaged Template silently uses a guessed paid model | Public run preflight | Run refuses until a complete Profile exists |
| Global availability is inert | Adding a Global Profile changes every repository's default run | Configuration resolution | Only Repository selection enables Reviews |
| Repository roll-up preserves order | Party expansion reorders specialized Reviews | Selection resolver | Expanded Bundle order matches Global then Repository file order |
| Exact overlap runs once | Two selected Parties duplicate cost and findings | Selection resolver | First scoped Profile occurrence remains and later exact occurrences are recorded as deduplicated |
| Cross-scope names remain distinct | Name-only deduplication drops a Repository Profile | Resolver and CLI preview | Both scoped identities run and produce a strong warning |
| Profile execution is stable | Ordinary flags change a benchmarked Profile's model | Public run command | Saved execution fields reach the Review Record unchanged |
| Party membership stays flat | Nested composition reintroduces cycles and inherited policy | Party validation | Party references to Parties fail before launch |
| Missing live references fail closed | A renamed Global Profile silently reduces coverage | Public run preflight | No Reviewer launches and the missing scoped identity is reported |
| Bundle preserves intent and execution | A changed Global Party makes history impossible to explain | Ledger round-trip | Authored selection and expanded Profile Revisions both survive inspection |
| Effective inspection is not authored inspection | An agent mistakes one file for what will run | Config CLI | `config show` reports expansion and provenance; `config file show` reports one document |
| Manual model entry remains possible | Incomplete discovery blocks a valid model | Profile editor and command | Warning can be confirmed and exact model ID is saved |
| Template updates are explicit | A product update overwrites customized judgment | Hub and Configuration Manager | No Profile changes before reviewed confirmation |
| Recovery preserves retired state | Fresh initialization destroys an incompatible ledger | Real filesystem recovery | Ledger and sidecars exist in backup before new state preparation |
| Hub cancellation is clean | Leaving a form publishes part of a Profile | Hub and Configuration Manager | Cancel and exit before final confirmation write nothing |
| Machine mutation never blocks on prompts | An agent waits forever without a terminal | Config command | Mutation refuses unless `--yes` supplies authorization |
| Slice 5: an explicit choice replaces the saved default | `--profile code-quality` silently appends to the repository roll-up | Public run command | Exactly the named Profile or Party executes; conflicting flags are refused |
| Slice 5: unqualified explicit names resolve Repository first | An explicit name launches the Global copy although a Repository copy exists | Selection resolver | The resolved scope follows Repository-before-Global; qualified names are exact |
| Slice 5: the roll-up owns its Concurrency Limit | A Party limit replaces the repository limit or vice versa | Selection resolver and bundle persistence | Expanded list carries the authoring limit's provenance |
| Slice 5: an unconfigured run refuses cleanly | A missing selection starts reviews with nothing selected or writes fallback state | Public run command | Nonzero exit names the configuration gap; no Reviewer launches and no files are written |

## Explicit non-goals

- No web configuration UI.
- No arbitrary dotted-key setter or Viper-style binding layer.
- No credential storage or automatic browser login.
- No executable packaged Profiles.
- No Profile variants or ordinary execution overrides.
- No Party inheritance, nesting, `extends`, or member execution pins.
- No automatic Global baseline execution.
- No Profile or Party rename in V1.
- No migration for superseded Profile, Party, configuration, or ledger formats.
- No silent Reviewer, model, Profile, Party, or capability substitution.
- No full-screen TUI for ordinary Review output.

## Tracked removals

The accepted replacement is expected to make tracked source and documentation
obsolete. Before deleting any tracked path, list the exact path and request the
approval required by `AGENTS.md`. Do not preserve dead readers, aliases, or
commands to avoid requesting deletion approval.
