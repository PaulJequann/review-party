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
review-party version
```

Pass a directory as the first argument to install elsewhere. The installer does
not create configuration or state. Sync the tracked Profile Templates into
complete executable Global Profiles for local dogfood with:

```sh
./scripts/sync-local-profiles.sh
```

This command replaces the local `bugs`, `code-quality`, and `documentation`
Profile metadata and instructions. It reads Template content and revisions from
the checkout, and fixes execution to the dogfood Reviewer settings. Environment
variables named `REVIEW_PARTY_DOGFOOD_REVIEWER`,
`REVIEW_PARTY_DOGFOOD_MODEL`, `REVIEW_PARTY_DOGFOOD_EFFORT`, and
`REVIEW_PARTY_DOGFOOD_DEADLINE` may explicitly select different settings. Pass
a Profiles directory as the first argument to target an isolated configuration.
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

> Configuration Hub Slices 5 through 7 resolve the repository's saved review selection
> and expose it through `review-party run` and `review-party config ...`.
> The transitional `review` and `party run` commands are replaced with no
> aliases. See
> [`docs/configuration-hub-implementation-plan.md`](docs/configuration-hub-implementation-plan.md).

Run `review-party` with no arguments for task-oriented help. Every command and
nested command supports `--help`, and `review-party completion
bash|fish|powershell|zsh` generates a shell completion script. Profile and Party
completion reads saved executable definitions from local configuration and
returns no Profile or Party names when none are saved. Reviewer completion is available for Eval experiment flags. Completion never
launches an Agent Harness.

Initialize Review Party once for the repository before the first Review. This
prepares managed per-user state without creating repository files or Profile
copies. Supplying `--state-dir` also records that advanced choice in Global
Configuration:

```sh
review-party init --repo .
```

Advanced callers may select a state location during initialization with
`--state-dir PATH`; Review Party remembers that choice in the selected user
configuration. Pass `--config PATH` consistently to init, Review, inspect, and
history when using a non-default configuration. Initialization is idempotent
for current state. Review, inspect, and history refuse to create state. Review Party is pre-release and does not upgrade retired
ledger schemas; select a fresh `XDG_STATE_HOME` or `--state-dir` and run
`review-party init` when an old ledger is incompatible.

List and explain saved executable Review Profiles without creating a Review
Record. `config discover` is the explicit bounded observational command in
this group and may launch a Reviewer harness; the other commands are local:

```sh
review-party profiles
review-party explain bugs
review-party config path
review-party config show --repo . --format json
review-party config file show --scope global --format json
review-party config validate --repo . --format json
review-party config discover --format json
```

For `config discover --format json`, specifying a Reviewer returns one
`Result` object. Omitting the Reviewer returns an object with a `results` array
containing one result per known Reviewer.

Agents and automation use the `config` command family for configuration
changes. Read the effective configuration with `config show`; use `config file`
only to inspect one authored document. Mutation commands return a semantic Plan
with before and after values. Confirm mutations in a TTY or pass `--yes`; JSON
and non-TTY mutations require `--yes`.

Review-selection `add` and `remove` accept qualified references only when they
match `--scope`: use `global:NAME` with `--scope global` and
`repository:NAME` with `--scope repository`. Unqualified references use the
selected scope.

For example, create a complete Profile from a packaged Template, add it to the
repository's saved selection, and set the selection limit:

```sh
review-party config profile create code-quality \
  --template code-quality \
  --reviewer opencode \
  --model meta/muse-spark-1.2-contributor \
  --effort high \
  --deadline 3m \
  --yes --format json
review-party config reviews add --scope global --profile code-quality \
  --repo . --yes --format json
review-party config reviews set-concurrency 2 --repo . --yes --format json
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

Each saved Profile fixes its Reviewer, model, reasoning effort, Attempt
deadline, and instructions. Ordinary `run`, explain, replay, and Party
commands do not accept execution overrides. A different cost or quality choice is a
differently named Profile.

Packaged `bugs`, `code-quality`, and `documentation` material is available only
as non-executable Templates. Global and Repository Configuration may still
enable or disable known Reviewers and constrain accepted models. Review Party
checks the saved Profile against that policy before launch and never substitutes
a different Reviewer or model.

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

Review Party currently invokes all four harnesses directly (Grok, OpenCode,
Copilot, Codex). ACPX remains a future transport option rather than part of
the current execution path.

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

## Operational Review Records

New Review Records use schema version 3 and retain machine-readable operational
facts alongside the canonical result. Canonical-v2 results expose an ordered
`findings` collection with each Reviewer claim's severity, category, validated
location string, failure, evidence, smallest safe correction, and regression
test intent; `finding_count` is derived from that collection. JSON inspection
also includes the exact runtime version/VCS information available from the built binary, immutable
Subject size facts, owned-phase and total timings, and a categorical termination
with its phase when a Review is incomplete. Review lifecycle remains limited to
Pending, Running, Completed, and Incomplete; execution phases are diagnostic
facts rather than additional states.

Review Party distinguishes unavailable Reviewers, authentication failures,
deadlines, cancellation, transport failures, malformed harness output, result
validation failures, and unknown failures without requiring callers to parse a
diagnostic string. Review Records are persisted in the managed SQLite ledger at
`$XDG_STATE_HOME/review-party/ledger.sqlite` (or the corresponding
`$HOME/.local/state` fallback). The CLI intentionally exposes no storage-path
selector; isolate tests and experiments with `XDG_STATE_HOME`. The ledger uses
one pre-release initial schema. Review Party fails on retired or colliding state
instead of importing or rewriting it.

## Artifact evidence

New Attempts keep bounded constructed prompts and decoded assistant text as
private files under the Review Party state root, while their Review Record holds
only relative paths, byte counts, SHA-256 digests, and truncation state. Use
`inspect --format json` for automation or ordinary `inspect` to see references;
add `--verify-artifacts` to reopen and validate every referenced file. Artifact
contents are intentionally not printed. The filesystem artifact store rejects
paths outside its configured root and reports missing or digest-mismatched files
as integrity failures. Treat artifacts as sensitive review context.

## Review Profiles

Review Party packages non-executable `bugs`, `code-quality`, and `documentation`
Templates. A Template supplies judgment instructions only. Profile Creation
copies those instructions, then requires the Caller to choose a Reviewer, model,
reasoning effort, and positive Attempt deadline.

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
current access. Discovery never logs in implicitly.
Where a harness requires it, discovery may use already configured, allowlisted
credentials for its read-only provider query; it never starts authentication or
stores credentials.
When a manually entered model is not among the immediately known choices,
Profile creation reports a warning and requires TTY confirmation or explicit
`--yes` authorization. Noninteractive callers must pass `--yes` to receive the
plan and warning in the command result; without it, the command exits before
displaying the warning. Copilot currently reports discovery as unsupported with
its documented login action.
