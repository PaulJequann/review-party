# Global Configuration v1

Status: accepted on 2026-10-28. Extends
[Profile library v1](profile-library-v1.md),
[Review Party baseline v1](baseline-v1.md), and
[Review Checkpoints v1](review-checkpoints-v1.md). Resolves the "Personal
Checkpoints" open question in the Checkpoints design and amends
[ADR 0001](../adr/0001-project-declared-review-checkpoints.md) through a new
ADR 0002.

## Problem

Global Configuration can hold Profiles, Parties, reviewer policy, and the
state directory. It cannot select Reviews or declare Checkpoints. Both are
Repository Configuration fields, and `validateScopeFields` in
`internal/configuration/document.go` rejects them in Global scope.

For a Caller who uses Review Party in many repositories this means every
repository carries a hand copy of the same choices. An audit of one machine on
2026-10-27 found nine `.reviewparty/config.json` files that select the same
Global Party or the same three Global Profiles, four of them with the same
`pre-push` Checkpoint repeated verbatim, one clone whose declared git hook was
never installed, and a `doctor` run that aborts on a zero-byte `.codex` entry.
A change made in one repository reaches no other.

The repository-local design stays right for teams. A committed selection and
Checkpoint are what a team agrees on. The gap is that there is no layer for
the one person who runs Review Party everywhere.

## Decision

Global Configuration becomes the Caller's default, and Repository
Configuration becomes the team's override.

| Field | Global scope | Repository scope | Effective value |
|---|---|---|---|
| `reviews` | Allowed. Only the `global` group may hold items. | Allowed, unchanged. | Repository `reviews` when present, else Global `reviews`, else none. |
| `checkpoints` | Allowed. | Allowed, unchanged. | Repository `checkpoints` when present, else Global `checkpoints` when the repository is enrolled, else none. |

A present field replaces the other scope's field as a whole. There is no
per-item merge. `config show` names one source file for each effective value,
and a reader of either file sees what applies without computing a union.

A repository is **enrolled** when it has a Repository Configuration file at
`.reviewparty/config.json`, even one holding only `{"schema_version": 1}`.
The Global selection applies in every repository, because `review-party run`
is an action the Caller takes on purpose. Global Checkpoints apply only in
enrolled repositories, because a hook that fires in every clone on the machine
would also fire in work repositories and tool checkouts the Caller never meant
to gate.

`integrations` on a Global Checkpoint is the Caller's floor for every enrolled
clone. `init` installs or reports it per clone, as it does for a team floor
today. Caller Agent hooks stay optional and unchanged in this design.

## Model

### Effective Review selection

`Loaded.ReviewSelection()` in `internal/configuration/domain_storage.go`
returns the Repository selection when the Repository document is present and
has `reviews`. Otherwise it returns the Global selection when the Global
document has `reviews`, with `Source: SourceGlobal` and the Global path.
Otherwise it returns the unauthored value it returns today.

`ResolvedReviews.Kind` gains `SelectionGlobalDefault`. `LimitSource` gains
`LimitGlobalSelection`. Every reader of `ErrNoReviewSelection` keeps its
behavior, and the follow-up guidance names both `config reviews add` and
`config reviews add --global`.

A Global `reviews` with a non-empty `repository` group is invalid, with the
reason "reviews.repository selects Repository definitions, which Global
Configuration cannot name".

### Effective Checkpoints

`Manager.Checkpoints(repository)` in `internal/configuration/checkpoint.go`
becomes the effective lookup described in the Decision table. Every current
caller already goes through it: the git and agent hooks, the installers,
`floor_gaps.go`, `doctor`, `init`, the first-use journey, and the engine's
`CheckCheckpoint` and `scopeCheckpoint`. None of them changes.

`checkpointReport` and the hook refusal name the file that declared the
Checkpoint, so a refusal under a Global Checkpoint says so.

### Enrollment

Enrollment is the presence of the Repository Configuration file. No new field.
`init` in an unenrolled repository whose Global Configuration declares
Checkpoints offers one step, "Enroll this repository in your Global
Checkpoints", which writes `{"schema_version": 1}` through an ordinary Plan.
Without a terminal the report prints `review-party init --enroll --yes`.

A hook in an unenrolled repository loads two small JSON files, finds no
Checkpoint, and exits 0 silently, as it does today for an undeclared one.

### AGENTS.md block

The block stops being generated from the declaration. It becomes fixed text
that tells the agent to run `review-party checkpoint check pre-push` before a
push and `review-party checkpoint check pre-commit` before a commit, and to
follow the next command the check prints. The check already knows what is
declared, what is exempt, and what is left. A fixed block cannot go stale, so
`hookBlockStale`, `gapStale`, and the replace-between-markers path are
deleted. The marked block stays so `uninstall` can remove it.

### What stays

- Repository Configuration keeps every field it has today.
- ADR 0001 still holds for a repository that declares Checkpoints. A team's
  declaration wins as a whole over the Caller's Global one.
- Hooks only check. No hook runs a Review. `--no-verify` still skips git hooks.
- `core.hooksPath` is not touched. Git hooks stay per clone, installed by
  `init`.
- Caller Agent hooks keep their current targets. The Claude Code personal
  target stays `.claude/settings.local.json`.

## Commands

| Command | Change |
|---|---|
| `config reviews add`, `remove`, `move`, `concurrency` | Gain `--target global`. Without it they edit the Repository selection as today. The existing `--scope` flag keeps its meaning, the selection group an item goes in, so a new flag names the file. |
| `config checkpoint set`, `remove` | Gain `--target global`, the same flag name for the same meaning. |
| `config show` | Prints the effective selection and Checkpoints with the scope and path each came from. |
| `init --global` | Prepares state and applies setup flags to Global scope. `init --global --baseline ...` creates the baseline Profiles and Party and selects the Party in the Global selection. It reads no repository. |
| `init` in a repository | The selection step is skipped when the effective selection resolves, including from Global. New enrollment step as described above. `--enroll` applies only that step. |
| `run` | Prints one line to stderr when the effective `pre-push` Checkpoint lists `git` and this clone has no hook. The line names `review-party init`. |
| `checkpoint install *` | Unchanged flags. They install for the effective Checkpoints. |
| `doctor` | Resolves Global selection names. Reports a `.codex` entry that is not a directory as a finding instead of aborting. Stops listing non-directory entries under the skills roots as skipped Templates. |

## Delivery

Four pull requests, in order. Each lands with its tests, its CodeScene scores
on touched source files, and one run of the `verify-review-party` skill
against the real binary in an isolated `HOME`.

### PR 1. Global Review selection

Files.

- `internal/configuration/document.go`: allow `reviews` in Global scope.
- `internal/configuration/domain_storage.go`: `Loaded.ReviewSelection()`
  falls back to Global; `validateReviewSelection` rejects a Global
  `repository` group.
- `internal/configuration/selection.go`: `SelectionGlobalDefault`,
  `LimitGlobalSelection`, and the `resolveDefaultSelection` kind and source.
- `internal/configuration/runtime_snapshot.go`: `savedSelectionIncludesParty`
  and `noSelectionError` read the effective selection, not the Repository
  document.
- `internal/configuration/intent.go` and `baseline.go`: `SetReviewSelection`
  gains `Target Scope`; `Baseline.Select` takes the target.
- `cmd/review-party/config_commands.go`, `config_review_mutations.go`,
  `init_baseline.go`, `standard_commands.go`, `init_report.go`: the
  `--target` flag, `init --global`, and report lines that name the Global
  source.
- `cmd/review-party/config_show.go`: effective selection with source.
- `docs/product-model.md`: the Global Configuration paragraph.

Tests.

- `internal/configuration/selection_test.go`: Global selection resolves with
  kind `global_default`; Repository `reviews` replaces it as a whole; a Global
  `repository` group is invalid; the no-selection error names both commands.
- `internal/configuration/runtime_snapshot_test.go`: a run in a repository
  with no Repository Configuration resolves the Global selection and loads its
  Parties.
- `cmd/review-party/config_commands_test.go`: `config reviews add --target
  global` writes `~/.config/review-party/config.json` and leaves the
  repository alone.
- `cmd/review-party/init_baseline_test.go`: `init --global --baseline` with
  execution flags creates four Profiles, the Party, and the Global selection,
  and a rerun writes nothing. `init` in a fresh repository afterwards reports
  the selection as bound and skips the selection step.

Verification on the binary. In an isolated `HOME`, run `init --global
--baseline` with an execution, create an empty git repository with one commit,
and run `review-party run --format json` there with a Reviewer stub. The
resolved selection reports kind `global_default` and the Global path.

### PR 2. Global Checkpoints and enrollment

Files.

- `internal/configuration/document.go`: allow `checkpoints` in Global scope.
- `internal/configuration/checkpoint.go`: effective `Checkpoints`, and
  `SetCheckpoint` and `RemoveCheckpoint` gain `Target Scope`.
- `cmd/review-party/checkpoint_commands.go` and `checkpoint_hook.go`: the
  report and refusal name the declaring file.
- `cmd/review-party/config_checkpoint_commands.go`: `--target`.
- `cmd/review-party/init_report.go`, `standard_commands.go`, and
  `internal/configurationhub/first_use_checkpoints.go`: the enrollment step
  and `--enroll`.
- `docs/adr/0002-caller-declared-checkpoints-for-enrolled-repositories.md`.
- `docs/design/review-checkpoints-v1.md`: the open question is resolved here.

Tests.

- `internal/configuration/checkpoint_test.go`: an enrolled repository without
  `checkpoints` gets the Global ones; a Repository `checkpoints` replaces them
  as a whole; an unenrolled repository gets none.
- `cmd/review-party/checkpoint_hook_test.go`: the git hook refuses an
  unreviewed push under a Global Checkpoint in an enrolled repository and stays
  silent in an unenrolled one. The refusal names the Global path.
- `cmd/review-party/checkpoint_declared_test.go`: `checkpoint install git`
  installs for a Global Checkpoint.
- `cmd/review-party/init_test.go`: `init` in an unenrolled repository with
  Global Checkpoints prints the enrollment command without a terminal, and
  `init --enroll --yes` writes exactly `{"schema_version": 1}`.

Verification on the binary. In the PR 1 environment, declare a Global
`pre-push` Checkpoint with `integrations: ["git"]`, run `init --enroll --yes`
and `checkpoint install git --yes` in the test repository, then push an
unreviewed commit to a local bare remote. The push is refused with the Review
command. Repeat in a second repository without `init`. The push passes and the
hook prints nothing.

### PR 3. AGENTS.md block, run hint, and doctor fixes

The plan said a fixed block. Reading `internal/engine/checkpoint.go` refuted
the premise: `review-party checkpoint check` on an undeclared Checkpoint runs
a strict check, so a fixed block that named both checks would misdirect agents
in a repository that declares only `pre-push`. The block is instead generated
from the declared Checkpoint names alone, one fixed line per name sending the
agent to `review-party checkpoint check <name>`, which reads the settings at
run time. Declaring or removing a Checkpoint still makes the block stale, so
the stale path stays.

Files.

- `cmd/review-party/checkpoint_agents_md_install.go`: `agentsMDLines` keyed
  by Checkpoint name and `agentsMDBody(names)`; delete the settings-driven
  generator (`agentsMDInstructions`, `agentsMDInstruction`, `agentsMDLine`,
  `fixRule`, `countedReviews`). `hookBlockStale` and `gapStale` stay.
- `cmd/review-party/run.go`: `missingGitHookHint`, one stderr line computed
  from `integrationGaps` for `git` only when the effective `pre-push` lists
  `git` and the hook is missing. It names `review-party init`.
- `cmd/review-party/checkpoint_agent_install.go`: `planAgentEntry` turns a
  `.codex` or `.claude` entry that is not a directory into a by-hand step
  instead of an error. This is where `doctor` aborted; `footprint.go` never
  did, so it is unchanged.
- `cmd/review-party/checkpoint_uninstall.go`: `planUninstall` treats a
  non-directory parent as nothing installed, so `doctor` also survives it with
  nothing declared.
- `cmd/review-party/doctor_findings.go`: a gap with by-hand steps lists them
  as `init` does.
- `internal/configuration/skill_template.go`: `addRoot` skips an entry that
  is not a directory before calling `loadSkillTemplate`, so a stray file under
  a skills root is not a skipped Template.

Tests.

- `cmd/review-party/checkpoint_agents_md_install_test.go`: changing every
  Checkpoint setting leaves the installed block byte-identical; declaring a
  Checkpoint makes it stale.
- `cmd/review-party/doctor_test.go`: a zero-byte `.codex` yields exit 0, no
  finding with nothing declared, and the by-hand finding with `codex`
  declared.
- `internal/configuration/skill_template_test.go`: a `.zip` under the skills
  root yields no skipped Template.
- `cmd/review-party/checkpoint_global_test.go`: the hint appears once when the
  effective `pre-push` lists `git` and no hook is installed, and not after the
  install.

Verification on the binary. `scratch/verify-pr3.sh` runs the doctor, hint,
block, and skills-root cases against the built binary in an isolated HOME.

### PR 4. Remove `defaults.reviewer`

Dropped. The premise was that no run path reads `defaults.reviewer`. It is
false: `applyConfiguredEvalReviewer` in `internal/engine/eval_configuration.go`
gives `review-party eval run` the configured default Reviewer, and through it
the Reviewer's model, when the experiment names none.
`TestEvalUserConfigurationControlsRuntimeDefaultsAndFlagsOverride` covers that
on purpose. Removing the field would change eval behavior, so the field stays
and is documented as the eval default Reviewer. Removing it is a separate
decision the Caller can take with the eval fallback replaced.

## Migration of this machine

No code migrates existing files. The product is pre-release. On the audited
machine the move is five commands and one edit per repository.

```sh
review-party config reviews add --target global --scope global --party baseline
review-party config checkpoint set --target global pre-push --requirement reviewed --unreviewed-lines 0 --review-budget 3 --waivers human --integration git
```

Then in each of the nine repositories, replace `.reviewparty/config.json` with
`{"schema_version": 1}` and run `review-party init --yes` to install the hook
where it is missing. A repository that needs a different selection or
Checkpoint keeps its own fields.

## Decisions made in this plan

- **Whole replacement, not per-item merge.** A merge needs a rule for order,
  duplicates, and the concurrency limit, and `config show` would have to
  explain a union. Replacement keeps each file self-describing. The cost is
  that a repository which wants the Global selection plus one more Profile
  repeats the Global items. That is the rare case.
- **Enrollment by file presence, not a field.** A field inside the file would
  still require the file. Presence is the minimal signal, and it already means
  "Review Party is set up here".
- **Selection applies everywhere, Checkpoints only when enrolled.** `run` is
  explicit. A hook is not.
- **No global `core.hooksPath`.** It needs a dispatcher for every hook name,
  it conflicts with dotfiles-managed git configuration, and repositories on
  husky or a local `hooksPath` escape it anyway. `init` per clone plus the
  `run` hint covers the gap at lower risk.
- **Caller Agent hooks unchanged.** They stay optional. No user-level Claude
  Code target and no SessionStart hook in this design.
- **`--target`, not `--global`.** `config reviews` already has `--scope` for
  the selection group, and `config profile` and `config reviewer` use `--scope`
  for the file. One more meaning for `--scope` on `reviews` would be ambiguous,
  so the file gets its own flag. Renaming the Profile and Reviewer flag to
  match is a follow-up.
- **Configuration Hub edits both files.** The Reviews and Checkpoints areas
  show one section per configuration file, and Profile and Party creation ask
  which file to write. Without `--repo` outside every git repository, the Hub
  opens on Global Configuration only, marked `[global only]`. It hides
  Repository sections, Profile copy, and the scope question, and shows "Run
  preview needs a repository" in place of the resolved run preview. An explicit
  `--repo` must still name a repository, and `init` never falls back.

## Out of scope

- Merging Global and Repository selections or Checkpoints.
- A machine-wide git hook.
- New Caller Agent Integrations or targets.
- Migration code for existing Repository Configuration.

## Needs sign-off

1. The Decision table, in particular whole replacement and enrollment by file
   presence.
2. The flag and command names `--target global`, `init --global`, and
   `init --enroll`.
3. PR 3 deletes the stale-block code paths. It needs path-specific deletion
   approval. PR 4 is dropped; see its section.
4. PR order. PR 1 alone already removes the duplicated selections on the
   audited machine.
