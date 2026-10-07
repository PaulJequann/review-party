# Review Party

Review Party is an experimental code-review CLI for compiling named review
profiles into bounded runs across different coding agents and transports.

The accepted domain language lives in [`CONTEXT.md`](CONTEXT.md), and the
confirmed product structure is captured in
[`docs/product-model.md`](docs/product-model.md). Primary-source investigations
live in [`docs/research`](docs/research/).

The accepted first implementation slice is documented in
[`docs/design/conductor-v1.md`](docs/design/conductor-v1.md).

## Local development installation

Build and atomically install the current checkout into
`${XDG_BIN_HOME:-$HOME/.local/bin}`:

```sh
./scripts/install-local.sh
review-party --version
review-party version
```

Pass a directory as the first argument to install elsewhere. The installer does
not create configuration or state. Sync the tracked Profile Templates into
complete executable Global Profiles for local dogfood with:

```sh
./scripts/sync-local-profiles.sh
```

This command replaces the local `bugs`, `code-quality`, `documentation`, and
`test-audit` Profile metadata and instructions. It reads Template content and
revisions from the checkout, and fixes execution to the dogfood Reviewer
settings. Environment variables named `REVIEW_PARTY_DOGFOOD_REVIEWER`,
`REVIEW_PARTY_DOGFOOD_MODEL`, `REVIEW_PARTY_DOGFOOD_EFFORT`, and
`REVIEW_PARTY_DOGFOOD_DEADLINE` may explicitly select different settings. Pass a
Profiles directory as the first argument to target an isolated configuration.
The default dogfood Attempt deadline is eight minutes.

Run the installed-binary smoke check from an isolated repository and isolated
XDG roots with:

```sh
./scripts/smoke-installed.sh "$(command -v review-party)"
```

Review Party is pre-release. Re-run the installer and Profile sync after
updating `main`, and use the installed binary rather than `go run` for ordinary
dogfood Reviews.

## Current CLI

> Review Party resolves the repository's saved review selection and exposes it
> through `review-party run` and explicit `review-party config ...` commands.
> The recurring Hub, `review-party config`, opens its
> scoped, searchable overview and focused Profile creation, Party creation,
> Repository Reviews, and Repository-to-Global Profile-copy flows in a
> terminal, and refuses without writes outside a terminal. The explicit
> commands below remain the automation interface.
> The transitional `review` and `party run` commands are replaced with no
> aliases. See
> [`docs/configuration-hub-implementation-plan.md`](docs/configuration-hub-implementation-plan.md).

Run `review-party` with no arguments for task-oriented help. Every command and
nested command supports `--help`, and `review-party completion
bash|fish|powershell|zsh` generates a shell completion script. Profile and Party
completion reads saved executable definitions from local configuration and
returns no Profile or Party names when none are saved. Reviewer completion is available for Eval experiment flags. Completion never
launches an Agent Harness.

Initialize Review Party once for the repository before the first Review.
`init` prepares managed per-user state and then brings the repository to a
runnable state. Supplying `--state-dir` also records that advanced choice in
Global Configuration:

```sh
review-party init --repo .
```

Start with the Review Party baseline. It is four Global Profiles created from
the `bugs`, `code-quality`, `documentation`, and `test-audit` Templates, plus a
Global Party named `baseline` that runs all four. `--baseline` creates whatever
is missing, then adds `global:baseline` to the repository's Review selection.
You choose one Reviewer, model, reasoning effort, and Attempt deadline for the
Profiles it creates; `review-party config discover <reviewer>` lists models:

```sh
review-party init --repo . --baseline \
  --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --yes
```

The execution flags are needed only while a baseline Profile is missing, and
they are rejected without `--baseline`. Existing Profiles keep their own
execution and instructions. Each step publishes through its own Plan, so a
rerun finishes a stopped one and writes nothing once all three exist. A
teammate who clones a repository that selects `global:baseline` runs the same
command to create the Profiles and Party on their machine; the committed
selection is not changed. A Global Party `baseline` with other members blocks
the command until you edit or remove that file.

In a terminal, `init` runs the first-use journey with the Configuration Hub's
forms. When the repository's Review selection names Global Profiles or Parties
that your Global Configuration lacks, it offers to create each one. A missing
Profile starts from the Template of the same name when one exists. When the
repository has no selection, it offers the Review Party baseline first, then
your existing Profiles and Parties, or Profile Creation. A chosen Global Party is offered as a Repository Party whose
members stay Global Profile references, so teammates bind Profiles rather than
invent the Party's members. Each step publishes through its own reviewed Plan,
so a cancelled `init` loses nothing a rerun cannot finish. Pass `--accessible`
for non-redrawing prompts. After the selection, the journey shows each declared
Review Checkpoint, or offers to declare one with the Integrations every
contributor installs. It then offers to install the Integrations those
Checkpoints list, and offers the Caller Agent hooks they do not list as
personal hooks, preselecting the agents found on `PATH`. The journey ends with
one line for each piece still missing.

Without a terminal, `init` writes no configuration. It prints each missing
piece on one line with the command that adds it, or
`Repository is ready: review-party run --repo PATH`. A declared Checkpoint
without a hook its Integrations list gets its own line naming `review-party
checkpoint install <integration>`, or the snippet to add by hand when the git
hook tool needs one. When a Checkpoint lists `codex`, a line names the Codex
`/hooks` approval step. When a Checkpoint lists `agents-md`, a missing or stale
block gets a line naming `review-party checkpoint install agents-md`.

`--profile NAME` and `--party NAME` add names to the Review selection without
the journey. Each is repeatable. An unqualified name resolves Repository before
Global, like `run`, and joins the selection group of that scope. A name already
selected changes nothing. The writes go through one Plan, which a terminal
confirms and which needs `--yes` without one:

```sh
review-party init --repo . --profile bugs --party global:crew --yes
```

Advanced callers may select a state location during initialization with
`--state-dir PATH`; Review Party remembers that choice in the selected user
configuration. Pass `--config PATH` consistently to init, Review, inspect,
history, status, and wait when using a non-default configuration.
Initialization is idempotent for current state. Review, inspect, history,
status, and wait refuse to create state. A
schema 10 or 11 ledger requires one `review-party init` run, which adds the
misses and finding verdict tables it lacks and keeps every recorded Review;
other commands refuse that ledger until
then, and `--backup-incompatible` refuses it because it is compatible. Review Party is pre-release and does not upgrade other retired ledger
schemas. Back up an incompatible ledger and its SQLite sidecars first, then
authorize fresh state in a separate command:

```sh
review-party init --repo . --backup-incompatible --yes
review-party init --repo . --fresh --yes
```

The first command moves the old bytes under the state root's `backups/`
directory and records that fresh initialization is pending. It does not claim
a migration or create a new ledger. Repeat the
same `--state-dir` and `--config` options on both commands when using either
advanced selector.

List and explain saved executable Review Profiles without creating a Review
Record. `config discover` is the explicit bounded observational command in
this group and may launch a Reviewer harness; the other commands are local:

```sh
review-party profiles
review-party explain bugs
review-party config path
review-party config show --repo . --format json
review-party config file show --scope global --format json
review-party config validate --repo .
review-party doctor --repo .
review-party config discover --format json
```

For `config discover --format json`, specifying a Reviewer returns one
`Result` object. Omitting the Reviewer returns an object with a `results` array
containing one result per known Reviewer.

Humans can open the recurring terminal shell for the current repository with
`review-party config`; pass `--repo PATH` for another repository and use
`--accessible` for the same configuration operations through non-redrawing
prompts in a terminal. An accessible form abort exits the Hub without
publishing; ordinary focused-form cancellation returns to the Hub with its
draft. Accessible mode still requires terminal input; non-TTY callers must use
explicit commands. Opening, searching, navigating, cancelling an editor, and
exiting the shell do not publish configuration. The Hub creates new Profiles
and Parties; it does not edit existing definitions. It assembles Repository
Reviews and copies a Repository Profile only to Global Configuration. Each
focused editor previews and confirms its Configuration Manager Plan before
atomic publication. Instruction editing uses `$EDITOR`; export an argv-style
editor command such as `export EDITOR=vim`. If it is unset or cannot launch, the
Hub reports the failure and retains the draft. Agents and automation use the
explicit `config` command family for configuration changes. Read the effective
configuration with `config show`; use `config file` only to inspect one authored
document. Mutation commands return a semantic Plan with before and after
values. Confirm mutations in a TTY or pass `--yes`; JSON and non-TTY mutations
require `--yes`.

Review-selection `add` and `remove` accept qualified references only when they
match `--scope`: use `global:NAME` with `--scope global` and
`repository:NAME` with `--scope repository`. Unqualified references use the
selected scope.

For the shortest human path to a working repository selection, open the Hub,
create a complete Profile from a Template, then add it under Repository Reviews
and publish the reviewed changes:

```sh
review-party config --repo .
review-party run --repo .
```

Automation can perform the same setup with explicit commands:

```sh
review-party config profile create code-quality \
  --template code-quality \
  --reviewer opencode \
  --model meta/muse-spark-1.2-contributor \
  --effort high \
  --deadline 3m \
  --yes
review-party config reviews add --scope global --profile code-quality \
  --repo . --yes
review-party config reviews set-concurrency 2 --repo . --yes
```

Run one exact saved Profile, one Party, or the repository's saved selection:

```sh
review-party run --profile bugs --repo .
review-party run --profile code-quality --repo .
review-party run --profile documentation --repo .
review-party run --party baseline --repo .
review-party run --repo .
```

Without `--profile` or `--party`, `run` executes the repository's complete
saved roll-up from `.reviewparty/config.json reviews`. An explicit choice
replaces that default for one run and never edits configuration. Unqualified
names resolve Repository before Global; prefix `global:` or `repository:` for
an exact scope. Exact duplicated Profile identities execute once — later
occurrences are recorded as deduplicated — while same-named Profiles from both
scopes are distinct: both run and produce a strong warning in human and JSON
output. The authored selection, expanded execution list, deduplication facts,
warnings, Concurrency Limit provenance, and every executed Profile Revision are
preserved in the Review Bundle (`rb_…`, inspectable via
`review-party inspect`). A missing Profile or Party, invalid Profile,
unavailable saved Reviewer, or rejected model fails closed before any Reviewer
launches.

`run`, `inspect`, and `replay` print findings first. Human output leads with
each Review's status, summary, and numbered findings, then its Profile,
Reviewer, and Subject. A Review Bundle prints one block per member, labelled
`<scope>:<profile>`, before its selection, warnings, and Subject. JSON output always has a top-level `reviews`
array, so `jq '.reviews[].findings'` works for a single Review and for a Bundle.
Bundle output adds a `bundle` object. Every member has an `id` from the moment
the Bundle exists; a member the Bundle stopped before it finished is
`incomplete` with a `termination` that says so. A member whose Review Record cannot be read keeps its
`id`, has lifecycle `unreadable`, and carries the cause in `read_error`; the
other members still print, and the command exits with status 1. With
`--format json`, `--full` includes each complete Review Record under `record`:
the raw result, patch, changed paths, passes, attempts, and artifact
references. Human `--full` output adds the raw result, artifact references,
changed paths, and patch. `run` and `replay` exit with status 2 when the Review
or Bundle is incomplete.

While it runs, `run` prints a lifecycle heartbeat on stderr. The first line
names the Review Bundle or Review and the `status` and `wait` commands that
follow it. Later lines report each member's transitions: pending, started,
each Attempt, and finished. Stdout carries only the final result, so `--format json` stays
parseable. Pass `--quiet` to suppress the heartbeat. A caller whose own tool
call times out before the run finishes can check or resume it from the ledger:

```sh
review-party status rb_...
review-party status --repo .
review-party wait rb_... --timeout 10m
```

`status` reports the lifecycle of a Review Bundle or Review and its members
without waiting. With no id it lists the repository's pending and running
Bundles and Reviews, newest first. `wait` blocks until the run finishes, then
prints it as `run` would have, with the same `--format` and `--full` options,
and exits with the same code. `status` exits 0 whatever the lifecycle, and 1
after printing when a member's Review Record cannot be read. `--timeout`
bounds how long `wait` blocks: once it passes with the run still pending or
running, `wait` exits 1 and the run keeps going. A run `wait` finds finished is
always reported, even if the timeout passed during that check.

Every Attempt transition writes the ledger, so a run that shows no progress
for longer than its longest Attempt deadline plus two minutes has most likely
lost its process. `status` marks such a run stale ("no progress for 12m0s; the
run may have died"). `status ID` on a stale run exits 1 after printing, and
`wait` exits 1 with the same message instead of waiting on it. The in-flight
listing (`status` without an ID) marks each stale run the same way but still
exits 0, because nothing retires a dead run and the listing would otherwise
fail for good.

Each saved Profile fixes its Reviewer, model, reasoning effort, Attempt
deadline, and instructions. Ordinary `run`, explain, replay, and Party
commands do not accept execution overrides. A different cost or quality choice is a
differently named Profile.

Packaged `bugs`, `code-quality`, `documentation`, and `test-audit` material is
available only as non-executable Templates. Global and Repository Configuration
may still enable or disable known Reviewers and constrain accepted models.
Review Party checks the saved Profile against that policy before launch and
never substitutes a different Reviewer or model.

The Hub and `review-party doctor --format json` report when a Profile's saved
Template revision differs from the packaged revision. Drift never blocks a
compatible Review. Apply an update explicitly:

```sh
review-party config profile update-template code-quality --scope global --yes
```

The update replaces `instructions.md` and records a new Profile Revision. It
keeps the saved Reviewer, model, effort, and deadline. The reviewed Plan warns
when the current instructions differ from the packaged replacement.

Query the local Review history using facts recorded at execution time:

```sh
review-party history --reviewer opencode --profile bugs
review-party history --repo . --lifecycle incomplete \
  --termination deadline_exceeded --since 2026-08-01T00:00:00Z
review-party history --subject SUBJECT_ID --format json
```

Filters combine conjunctively. Repository paths resolve to their canonical Git
root, timestamps use RFC3339, and results are ordered newest-first by creation
time and then Review ID. The default limit is 20 and the maximum is 200. JSON
output contains `entries`, the applied `limit`, and `has_more`. History selects
existing Reviews; it does not rerun or replay them.

Record a bug that a completed Review missed, so later work can measure what each
Profile fails to catch:

```sh
review-party miss add --review rp_... --path internal/store/store.go --line 42 \
  --source codex-pr --description "nil map write on first save"
review-party miss add --review rb_... --profile bugs --path main.go \
  --source human --description "leaked file handle"
review-party miss list --repo . --profile bugs
review-party miss remove ms_... --reason "intended behavior"
```

`--source` is one of `codex-pr`, `human`, `incident`, or `other`. A miss
attaches only to a completed Review. A Review Bundle id attaches one miss to
every member Review, or only to the member named by `--profile`; the whole
request fails without writes when a targeted member has no completed Review.
`--profile` takes a bare Profile name or the `global:bugs` form the Bundle's
report prints. A bare name that matches more than one member, such as
`global:bugs` and `repository:bugs`, fails and lists the qualified choices.
`--path` must stay inside the repository: absolute paths and paths that climb
out with `..` fail, and the path is stored in cleaned form, so
`./internal/../internal/a.go` is stored as `internal/a.go`.
Each miss reads its repository, Subject, and Profile from the Review record
rather than copying them. `--recorded-by` and `--removed-by` default to the OS
username. Removal keeps the miss as a tombstone with its reason, remover, and
time; removing it again keeps the first tombstone. `miss list` hides removed
misses unless `--include-removed` is given, resolves `--repo` like `history`,
and orders by recording time and then recording order. `inspect` prints each Review's
active misses beneath its findings, and its JSON `reviews` entries carry them as
`misses`. That array is always present and is empty for Reviews that `run` and
`replay` just created. An unreadable Bundle member still shows its misses; if
they cannot be loaded either, its `read_error` says so and the other members
still print.

Record a verdict on each finding of a Review, so later work can measure which
findings a Profile gets right:

```sh
review-party finding record rp_... <<'EOF'
1 accept nil map write is reachable from the handler
2 reject the caller already checks the length
EOF
review-party finding list --repo . --profile bugs
```

Each stdin line is `N accept|reject|defer REASON`, where `N` is the finding
number the report prints and `REASON` is one line of 1 to 240 bytes. The lines
record together or not at all: a malformed line, a repeated finding number, or a
Bundle id exits 2, and a finding number the Review lacks or an unknown Review
exits 1, each without writes. Any Review with findings takes verdicts, including
an incomplete one. Repeating a finding's current verdict records nothing, and a
different verdict supersedes it; output is `recorded N` with the unchanged and
changed counts when there are any. A verdict judges the finding's text, so when
a Review is saved again with different text under that number, the verdict
shows as `stale` in `finding list` and the finding counts as unjudged.
`--recorded-by` defaults to the OS username.

`inspect` and `wait` print each current verdict beneath its finding, and JSON
findings carry it as `verdict` with `value` and `reason`. While any finding in
the report is unjudged, the report ends with a `feedback:` line (JSON
`feedback`) naming the command to record verdicts; a Bundle's hint says
`REVIEW` in place of a member id, since each member numbers its findings
separately. `wait` prints misses too; if it cannot load misses or verdicts, it
warns on stderr, prints the result without them, and exits as `run` would.

## Global and Repository Configuration

Review Party loads Global Configuration from
`${XDG_CONFIG_HOME:-$HOME/.config}/review-party/` and Repository Configuration
from `<repo>/.reviewparty/`. Both use schema version 1. Global Configuration may
hold reusable Profiles, Parties, Reviewer policy, Eval defaults, and the managed
state location. Global availability enables no Reviews by itself. The `reviews`
field is Repository-only; Global Configuration rejects it.

`defaults.profile` and `defaults.party` are not accepted configuration fields.
Repository Configuration owns the complete default roll-up in `reviews`:

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

Each selection item names exactly one Profile or Party. Global entries expand
before Repository entries in authored order; global items select Global
definitions and repository items select Repository definitions. The saved
selection owns its own Concurrency Limit for the complete roll-up, and this is
what `review-party run` executes without flags.

Only Global Configuration accepts `state_directory` and `eval`. Repository
Configuration rejects those fields. Unknown fields, unsupported versions,
unknown Reviewers, and invalid selections fail closed. Loading or resolving
absent configuration creates no files.

Review Party currently invokes all five harnesses directly (Grok, OpenCode,
Copilot, Codex, Claude Code). ACPX remains a future transport option rather
than part of the current execution path. The Claude Code Reviewer runs with
only the Read, Grep, and Glob tools, `--restricted` and `--safe-mode`
isolation, no MCP servers, and no session persistence. That isolation also
turns off Claude Code's own instruction loading, and restoring it would load
the reviewed revision's `.claude/settings.json`. Review Party instead appends
the repository-root `CLAUDE.md` and `AGENTS.md` to the system prompt, capped
at 32 KiB like Codex's default, and fails the Attempt if either is a symlink
that escapes the repository. It authenticates through Claude Code's own login
or an allowlisted `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, or
`CLAUDE_CODE_OAUTH_TOKEN`.

Review an exact committed range with full object provenance and an isolated
repository view:

```sh
review-party run --profile bugs --repo . --base HEAD~1 --head HEAD
```

Both revisions must resolve to commits already present in the local repository.
The Review Record stores their full object IDs, the binary-capable diff,
changed paths, Subject identity, and size facts. The Reviewer runs in a
short-lived detached worktree at the recorded head, so later caller changes do
not affect repository reads. Review Party does not copy ignored files,
dependencies, or untracked `.env` files and does not install packages. Reviewer
adapters receive explicit environment allowlists. The owned worktree is removed
after the Reviewer process exits on successful and incomplete outcomes; bounded
reconciliation handles verified inactive leftovers without touching unrelated
worktrees.

Replay one recorded committed Review's frozen inputs as a new ordinary Review:

```sh
review-party replay rp_...
```

Replay reuses the exact recorded committed Subject, Profile Revision, Profile
Snapshot, Reviewer, model, effort, capability contract, and execution deadline.
It first proves that the recorded commits still reconstruct the same Subject
identity. Replay rejects execution overrides and never substitutes an unavailable
recorded choice. Working-changes Reviews are not
replayable. The new record has its own lifecycle, result, attempts, timestamps,
and runtime provenance plus `replays_review_id` linkage to its source. Replay
reproduces experiment inputs, not non-deterministic model output.

Run the packaged general evaluation suite against one explicit Experiment
Configuration without installing Review Party into the shell:

```sh
go run ./cmd/review-party eval run global:general-bugs \
  --profile bugs \
  --reviewer opencode \
  --model meta/muse-spark-1.2-contributor \
  --effort high \
  --deadline 3m \
  --format json
```

Run the maintainability benchmark with the actual `code-quality` Profile:

```sh
go run ./cmd/review-party eval run global:code-quality \
  --profile code-quality \
  --reviewer opencode \
  --model meta/muse-spark-1.2-contributor \
  --effort high \
  --deadline 3m \
  --format json
```

The general suite uses multi-file repositories, cross-file contracts, and
adversarial known-clean changes. For a fast plumbing and result-contract check,
run the intentionally elementary `global:canary-bugs` suite instead. Canary
scores must not be presented as representative Reviewer quality.

Use a suite directory path for Caller-owned global or project cases. Pass
`--experiment PATH` to load a version-controlled configuration containing
`schema_version`, `name`, and an `experiment` object with the Profile,
Reviewer, model, effort, deadline, Retry Policy, and Concurrency Limit. Explicit
command flags override that configuration. The effective `retry_policy` and
numeric `concurrency_limit` are frozen with the Suite Run. Retries retain one
Eval Run and ordinary Review, preserve every Attempt, never substitute a
Reviewer or model, and finish Incomplete when exhausted. Backoff releases
Reviewer capacity. Inspect suite, case, or
adjudication revision records afterward; `eval inspect` dispatches by the
`esr_`, `er_`, or `ar_` ID prefix:

```sh
go run ./cmd/review-party eval inspect esr_... --format json
go run ./cmd/review-party eval inspect er_... --format json
go run ./cmd/review-party eval inspect ar_... --format json
```

Suite inspection reports an explicit `pending`, `running`, `completed`, or
`incomplete` lifecycle. All Eval Runs are created in manifest order before the
first Reviewer launch, so inspection can distinguish planned `pending` work,
the active `running` case, and terminal case outcomes without inferring state
from timestamps. A completed suite may retain Incomplete case outcomes; a suite
is itself Incomplete only when cancellation or a hard error stops planned work.
Eval Run JSON includes `updated_at`; Eval Suite Run JSON includes `lifecycle`
and optional `termination` details. Human inspection prints `review not started`
whenever `review_id` is empty, including Pending, Running, or Incomplete cases
that failed before ordinary Review creation.

Every Eval Case runs as an ordinary Review over a Git-free Synthetic Review
Subject. Expected Findings and source Git provenance never enter the Review
prompt or execution checkout. Eval execution records completion categories and
leaves semantic adjudication, scoring, and comparison for separate operations.
Invalid result contracts remain Incomplete operational outcomes even if their
raw assistant artifact discusses a plausible bug; semantic scores never infer a
Finding from malformed output.

`global:seeded-bugs` contains deterministic controlled-defect cases. Each seed
pins a full source commit, a reviewed patch digest, and the exact paths it may
change. Preflight creates an isolated temporary Git worktree, verifies and
applies the patch, removes Git authority from the resulting Synthetic Review
Subject, and then uses the same ordinary Review, retry, lifecycle,
adjudication, and scoring path as historical and known-clean cases.
See [`docs/design/evals-v1.md`](docs/design/evals-v1.md).

Human adjudication maps the structured Review Findings to the stored expected
Findings and then publishes an immutable basic score:

```sh
go run ./cmd/review-party eval adjudication export esr_... > decisions.json
# Edit the explicit expected and reported dispositions and add notes.
go run ./cmd/review-party eval score esr_... \
  --adjudication decisions.json --format json
go run ./cmd/review-party eval inspect ar_... --format json
```

Incomplete cases remain unscored. Recall, precision, clean-case behavior, and
completion each retain explicit numerators and denominators; uncertain decisions
and termination categories remain separate. See
[`docs/design/adjudication-v1.md`](docs/design/adjudication-v1.md).

Compare two stored adjudicated experiments over their exact shared case
revisions. The report shows baseline/candidate values and candidate-minus-
baseline deltas for quality, completion, termination, and runtime, while
listing omitted or mismatched cases. It preserves each side's Profile,
Reviewer/model/effort, transport, harness, and build provenance and never
declares a universal winner:

```sh
go run ./cmd/review-party eval compare \
  --baseline ar_... \
  --candidate ar_... \
  --format json
```

See [`docs/design/comparison-v1.md`](docs/design/comparison-v1.md) for the
matching and interpretation contract.

## Review Parties

A Party is a named ordered group of scoped Profile references with one positive
Concurrency Limit:

```json
{
  "schema_version": 1,
  "name": "release-gate",
  "description": "Pre-delivery review set",
  "concurrency_limit": 2,
  "profiles": [
    {"scope": "global", "profile": "bugs"},
    {"scope": "repository", "profile": "supabase-rls"}
  ]
}
```

Repository Parties live under `.reviewparty/parties/`. Global Parties live
under `${XDG_CONFIG_HOME:-$HOME/.config}/review-party/parties/`. A Global Party
may reference only Global Profiles. A Repository Party may reference either
scope.

Parties cannot contain Parties, use `extends`, inherit concurrency, or override
a member's Reviewer, model, effort, deadline, or instructions. Strict decoding
rejects those retired fields. Repository Configuration performs composition by
selecting Global and Repository Profiles or Parties in its `reviews` arrays.
See [`docs/design/party-v1.md`](docs/design/party-v1.md).

## Review Checkpoints

A Review Checkpoint is a point in the workflow, before push or before commit,
at which the repository expects its Review selection to have covered the
change. A Checkpoint checks for completed Reviews. It never starts one. Declare
Checkpoints in Repository Configuration, so the whole team shares them. Global
Configuration rejects them.

```sh
review-party config checkpoint set pre-push --exempt '*.md' --exempt 'docs/**' --waivers human
review-party config checkpoint remove pre-push
```

Both commands show the configuration Plan and confirm it in a terminal or with
`--yes`. `--unreviewed-lines N` (default 0) is the allowance: after a Review,
a change with at most N unreviewed lines passes without another Review, so a
small fix does not start another round. A binary file never fits the
allowance. `--review-budget K` (default 3, at most 9) caps the Reviews one
change may spend; a change that reaches it stops for a person. Both values
are always written, and a declaration missing either is rejected. The budget
counts per Profile and per Checkpoint, with no accounting across Checkpoints,
so declare it on one Checkpoint, pre-push or pre-commit, not both.
`--waivers` is `human` (the default), `anyone`, or `none`.

A Checkpoint measures what each selected Profile has not reviewed. Every
completed Review records, per path, the content it started from and the
content it reached, as Git blob IDs. The Checkpoint follows those records from
the change's base to the newest state each Profile reached and counts the
lines from there to the current content: the unreviewed lines. Rewording,
squashing, or rebasing commits that leave the content intact keeps the chain,
a Review of working changes credits the commit that records them, and an
Incomplete Review spends budget and credits nothing.

`--requirement` is `reviewed` (the default) or `judged`. Under `judged`, the
covering Reviews also need a current verdict on every Finding, recorded with
`review-party finding record <id>`. Accepted, rejected, and deferred all
count. Any covering Review whose Findings all have verdicts passes, so judging
the Review you just ran is enough. A verdict recorded before a Finding's text
changed no longer counts. A Finding whose location is one of the change's
exempt paths needs no verdict.

An exempt path pattern is relative to the repository root and separated by
`/`. Each segment is a Go `path.Match` pattern, and a `**` segment matches any
number of directories. A pattern without `/`, such as `*.md`, matches the file
name at any depth. Exempt paths leave both the change and each Review's
recorded change before they are compared. A change whose every path is exempt
passes.

`review-party checkpoint check pre-push` reports whether the change is
covered, residual (unreviewed lines within the allowance), exempt, waived,
unjudged, running, spent, or missing a Review, with the next command. The
next command for unreviewed lines is `review-party run --unreviewed`, which
reviews only what each Profile has not reviewed yet, skips Profiles with
nothing left, and exits 0 with one line when nothing is unreviewed. A spent
budget prints no command: a person decides. `--format json` adds, per
Profile, the Reviews in the chain, the unreviewed lines per path, and the
budget spent, and lists the unjudged Finding ordinals. When a change should
pass without its Reviews, record a waiver with a reason:

```sh
review-party checkpoint waive pre-push --reason "revert of a reviewed change"
```

A waiver applies to the exact unreviewed delta it was recorded for. Under
`human`, a person confirms it in a terminal, and `--yes` does not stand in
for that person. Under `none`, only Reviews pass the Checkpoint.

`review-party checkpoint install git` adds the git hook through the hook tool
the repository already uses. It checks for lefthook, husky, the pre-commit
framework, `core.hooksPath`, and plain `.git/hooks`, in that order. For hook
scripts, it inserts a marked block after the shebang and never changes the
existing lines. A pre-push hook that reads git's ref lines still receives them
unchanged. For lefthook and the pre-commit framework, it prints the snippet to
add to their configuration by hand. lefthook skips a pre-push command when
`HEAD` has no file changes against `@{push}`, so it does not check a push of
another branch from an up-to-date `HEAD`. Installed hooks load each Caller's
default Global Configuration, not `--config`. Rerunning the installer changes
nothing, and a block someone edited is left alone.

The hook refuses a change with unreviewed lines beyond the allowance, a spent
budget, or unjudged Findings, with one line that names the Checkpoint and the
next command. Only a spent budget adds the waive command, and only under
`anyone`. When the hook
cannot decide, because `review-party` is not on `PATH`, the configuration does
not load, or git cannot name a base, it warns on one line and allows the push
or commit.

Claude Code and Codex get the same Checkpoints from a `PreToolUse` hook that
runs before the agent's shell runs `git push` or `git commit`:

```sh
review-party checkpoint install claude-code
review-party checkpoint install codex
```

Each writes to the team file, which is `.claude/settings.json` or
`.codex/hooks.json`. Each agent gets one entry that every declared Checkpoint
shares. The Claude Code entry's `if` rule, `Bash(git *)`, starts the hook only
for git commands. `--personal` writes your own
file instead, which is `.claude/settings.local.json` or
`$CODEX_HOME/hooks.json` (`~/.codex/hooks.json` by default). The installer
appends to the file and keeps every existing byte. An installed entry is left
as is, and an edited review-party entry is reported and left alone. Both
entries fail open: while `review-party` is not on `PATH`, Claude Code shows a
one-line warning and Codex stays silent, and no exit status from
`review-party` blocks a command.
`--integration claude-code` or `--integration codex` on `config checkpoint set`
lists an agent in the team floor.

The hook reads the shell command the agent is about to run. `git push` decides
pre-push and `git commit` decides pre-commit, in the repository the command
runs in. A refusal reaches the agent as a JSON deny whose reason is the same
one line the git hook prints. The hook also refuses two forms it cannot check,
and names the command to run instead:

- A `git push` or `git commit` chained after another command, as in
  `git add -A && git commit -m x`. Run it as its own command. A plain `cd`
  before it, as in `cd app && git push`, is not another command: the hook
  decides the push in `app`.
- A `git commit` of paths or picked hunks, such as `git commit file.go` or
  `git commit -p`. Stage the change and commit without paths. `git commit -a`
  is checked against the tracked working-tree changes.

Codex runs a new or changed hook only after you trust it. Open Codex in the
repository and run `/hooks`. Codex also runs the hook before every shell
command, not only git ones, and starts a login shell to do it. On the machine
measured, every Codex shell command waits about 70 ms more, most of it that
login shell. Under Claude Code, each git command waits about 19 ms more. See
[`docs/design/review-checkpoints-v1.md`](docs/design/review-checkpoints-v1.md).

Agents read their instructions before any hook runs, so `agents-md` puts the
Checkpoints there too:

```sh
review-party checkpoint install agents-md
```

It writes a block between `<!-- review-party checkpoints: begin -->` and
`<!-- review-party checkpoints: end -->` into `AGENTS.md` at the repository
root, or into `CLAUDE.md` when only that exists. The block has one line per
declared Checkpoint naming the command that satisfies it, adds `review-party
finding record <id>` under `judged`, and mentions waivers only under `anyone`. A missing block is appended after one blank line, and the
rest of the file keeps every byte. After a Checkpoint declaration changes, the
block is stale and the installer replaces only the lines between the markers,
which a terminal confirms and which needs `--yes` without one. Unbalanced or
repeated markers are left for you to fix by hand. `--integration agents-md` on
`config checkpoint set` lists the block in the team floor.

`review-party checkpoint uninstall` takes back out what the installers added,
and nothing else:

```sh
review-party checkpoint uninstall
review-party checkpoint uninstall --undeclared --yes
```

It removes the marked hook blocks, the `PreToolUse` entries, and the
`agents-md` block, and keeps every other byte of each file. It deletes a file
only when install created it in this clone and nothing else was added to it
since. Install records the files it creates in
`.git/review-party-created.json`, and uninstall deletes that record once it
lists nothing. If that record is damaged, uninstall edits the files it would
have deleted, says so, and drops the record. A file you had before install is
only edited. A block or entry
someone edited, unbalanced `agents-md` markers, and lefthook or pre-commit
framework configuration are reported for you to remove by hand, and the
command exits 1 while any of them is left. It prints what it will change
before it changes anything, which a terminal confirms and which needs `--yes`
without one. Rerunning it is safe, and a run with nothing left prints
`Nothing to remove.` and exits 0.

`--undeclared` removes only what no declared Checkpoint uses: the hook blocks
of undeclared Checkpoints, and the agent entries and `agents-md` block once no
Checkpoint is declared. `config checkpoint remove` offers that in a terminal
and prints the command otherwise. `$CODEX_HOME/hooks.json` serves every
repository on the machine, and a `core.hooksPath` outside the clone or set in
global or system git configuration may serve several, so uninstall only names
those hooks unless you pass `--shared`.

`review-party doctor` reports what the declared Checkpoints still lack in this
clone, one line per finding, each ending in `; fix: <command>`:

- Profile and Party names the selection uses that no configuration defines.
- Team-floor Integrations that are missing, edited, not executable, or waiting
  on a hook tool to be activated.
- A stale or broken `agents-md` block.
- A Markdown exemption on a Checkpoint whose repository selects a documentation
  Profile, since those files would pass the Checkpoint without that Review.
- Installed hook blocks, agent entries, and `agents-md` blocks that no
  declared Checkpoint uses, fixed by `review-party checkpoint uninstall
  --undeclared`.
- Waivers recorded in this repository in the last 30 days, which are records
  and carry no fix.

```text
configuration is valid
Checkpoint pre-push has no claude-code hook; fix: review-party checkpoint install claude-code --repo /src/app
```

`--format json` returns the same findings in `unresolved_names`,
`integration_gaps`, `exemption_conflicts`, `undeclared_integrations`, and
`recent_waivers`. Doctor exits 1 only when the configuration is invalid.
Findings exit 0, because whether one blocks delivery is for the repository's
instructions and the Caller to decide. Doctor reads the ledger for Waivers but
never creates state.

## Operational Review Records

New Review Records use schema version 3 and retain machine-readable operational
facts alongside the canonical result. Canonical-v2 results expose an ordered
`findings` collection with each Reviewer claim's severity, category, validated
location string, failure, evidence, smallest safe correction, and regression
test intent; `finding_count` is derived from that collection. Each `reviews`
entry of `inspect --format json` carries the collection as `findings`, plus a
categorical termination with its phase when a Review is incomplete. `inspect
--full --format json` adds the complete record under `record`, with the exact
runtime version/VCS information available from the built binary, immutable
Subject size facts, and owned-phase and total timings. Review lifecycle remains
limited to Pending, Running, Completed, and Incomplete; execution phases are
diagnostic facts rather than additional states.

Review Party distinguishes unavailable Reviewers, authentication failures,
deadlines, cancellation, transport failures, reviewer input that exceeds the
Reviewer's size limit, malformed harness output, result validation failures, and
unknown failures without requiring callers to parse a
diagnostic string. Before launch, `run` measures the prepared input against a
Reviewer's declared size limit, ends the Review as incomplete without spawning
the Reviewer when the input is over it, and warns on stderr from 60 percent of
the limit. Review Records are persisted in the managed SQLite ledger at
`$XDG_STATE_HOME/review-party/ledger.sqlite` (or the corresponding
`$HOME/.local/state` fallback). The CLI intentionally exposes no storage-path
selector; isolate tests and experiments with `XDG_STATE_HOME`. The ledger
schema is the pre-release initial schema plus additive migrations that
`review-party init` applies. Review Party fails on retired or colliding state
instead of importing or rewriting it.

## Artifact evidence

New Attempts keep bounded constructed prompts and decoded assistant text as
private files under the Review Party state root, while their Review Record holds
only relative paths, byte counts, SHA-256 digests, and truncation state. Use
`inspect --full --format json` for automation or `inspect --full` to see
references. Without `--full`, `inspect` omits the patch and artifact references.
Add `--verify-artifacts` to reopen and validate every referenced file. Artifact
contents are intentionally not printed. The filesystem artifact store rejects
paths outside its configured root and reports missing or digest-mismatched files
as integrity failures. Treat artifacts as sensitive review context.

## Review Profiles

Review Party packages non-executable `bugs`, `code-quality`, `documentation`,
and `test-audit` Templates. A Template supplies judgment instructions only.
Profile Creation copies those instructions, then requires the Caller to choose a
Reviewer, model, reasoning effort, and positive Attempt deadline.

Every executable Profile is a two-file aggregate:

```text
.reviewparty/profiles/security/
├── profile.json
└── instructions.md
```

Global Profiles use the same shape under
`${XDG_CONFIG_HOME:-$HOME/.config}/review-party/profiles/`. Metadata is strict:

```json
{
  "schema_version": 1,
  "name": "security",
  "reviewer": "opencode",
  "model": "meta/muse-spark-1.2-contributor",
  "reasoning_effort": "high",
  "attempt_deadline": "3m",
  "template_id": "bugs",
  "template_revision": "bugs-v4"
}
```

`template_id` and `template_revision` are omitted together for a blank Profile.
The Configuration Manager publishes metadata and instructions through one
snapshot-bound atomic Plan. A stale plan or failed write changes neither file.
Profile Creation commands over this accepted aggregate are available through
`config profile create` and `config profile copy`. Agents can also use the
Configuration Manager's typed `PlanProfileCreation`, `PlanProfileCopy`, and
`Publish` operations.

The retired `profiles/<name>.md` representation has no Configuration Manager
reader. Use `config profile create` or `config profile copy` for agent-facing
Profile operations. See [`docs/design/profile-library-v1.md`](docs/design/profile-library-v1.md).

Use `review-party config discover [REVIEWER]` for a bounded provider-read-only
observation of available models and authentication status. It does not mutate
Review Party-owned configuration or store credentials. The external harness may
read its configured credentials and write its own state under its normal
HOME/XDG/CODEX_HOME locations. Successful results may be cached under the user
cache root for responsive selection; the cache is advisory and does not prove
current access. Cached choices expire after 24 hours, and entries without a
usable observation timestamp are never served. Discovery never logs in
implicitly.
Pass `--refresh` to discard the cached result for the observed Reviewer (or
every known Reviewer when none is named) before the observation runs. A cache
write still in flight from an earlier observation is invalidated as well, so a
refreshed observation that fails never resurrects the discarded entry.
Where a harness requires it, discovery may use already configured, allowlisted
credentials for its read-only provider query; it never starts authentication or
stores credentials.
When a manually entered model is not among the immediately known choices,
Profile creation reports a warning and requires TTY confirmation or explicit
`--yes` authorization. Noninteractive callers must pass `--yes` to receive the
plan and warning in the command result; without it, the command exits before
displaying the warning. Copilot currently reports discovery as unsupported with
its documented login action. Claude Code reports its authentication status and
harness version but has no model-list interface, so its models are entered
manually.
