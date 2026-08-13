# Review Party

Review Party is an experimental code-review CLI for compiling named review
profiles into bounded runs across different coding agents and transports.

The accepted domain language lives in [`CONTEXT.md`](CONTEXT.md), and the
confirmed product structure is captured in
[`docs/product-model.md`](docs/product-model.md). Primary-source investigations
live in [`docs/research`](docs/research/).

The accepted first implementation slice is documented in
[`docs/design/conductor-v1.md`](docs/design/conductor-v1.md).

## Current CLI

Initialize Review Party once for the repository before the first Review. This
prepares managed per-user state without creating repository files or Profile
copies:

```sh
review-party init --repo .
```

Advanced callers may select a state location during initialization with
`--state-dir PATH`; Review Party remembers that choice in the selected user
configuration. Pass `--config PATH` consistently to init, Review, inspect, and
history when using a non-default configuration. Initialization is idempotent,
and Review, inspect, and history refuse to create or migrate state.

List and explain the built-in Review Profiles without launching an Agent
Harness or creating a Review Record:

```sh
review-party profiles
review-party explain bugs
review-party explain documentation --reviewer copilot
review-party config path
review-party config show
```

Run a bug, code-quality, or Documentation Review with an explicitly selected
direct adapter:

```sh
review-party review bugs --reviewer grok
review-party review code-quality --reviewer opencode \
  --model meta/muse-spark-1.2-contributor --effort high
review-party review documentation --reviewer opencode \
  --model opencode-go/deepseek-v4-flash \
  --effort high
```

`bugs` remains the default Profile, and Grok is the default Reviewer. The
packaged `bugs`, `code-quality`, and `documentation` Profiles require the same
repository read/search capability contract with
explicit repository-mutation, shell, and web denials, but compile distinct
purposes, materiality thresholds, Passes, prompts, and Profile Revisions.
Review Party rejects an incompatible Reviewer before launch and does not
silently fall back. An unavailable compatible Reviewer produces an inspectable
Incomplete Review. Grok's built-in model is `grok-4.5`. OpenCode requires a
model supplied by user configuration or explicit `--model`; the product does
not compile a personal OpenCode model preference into its catalog. Copilot's
built-in `auto` selection records the model it resolves.

Callers may override the selected Reviewer's reasoning effort with
`--effort`. For OpenCode, Review Party passes an explicit value such as `high`
as the model variant and records it in the effective Profile Revision and
Review Record. Copilot's `auto` model cannot be combined with an explicit
effort; Review Party reports that unsupported choice instead of dropping it.

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

## User configuration

Review Party loads user policy from
`${XDG_CONFIG_HOME:-$HOME/.config}/review-party/config.json`. The versioned JSON
document may set one Default Reviewer and enable, disable, select, or restrict
models for each supported Reviewer. An advanced initialization may also add a
`state_directory` field; ordinary callers should let `review-party init`
manage it:

```json
{
  "version": 1,
  "default_reviewer": "grok",
  "reviewers": {
    "grok": {"enabled": true, "model": "grok-4.5"},
    "opencode": {
      "enabled": true,
      "model": "meta/muse-spark-1.2-contributor",
      "allowed_models": [
        "meta/muse-spark-1.2-contributor",
        "opencode-go/deepseek-v4-flash"
      ]
    },
    "copilot": {"enabled": true, "model": "auto"}
  }
}
```

An explicit `--reviewer` never bypasses `enabled: false`, and an explicit
`--model` must belong to `allowed_models` when that list is configured. Unknown
fields, unsupported versions, unknown Reviewers, disabled defaults, and
disallowed models fail before Review Subject resolution or Agent Harness
launch. A missing file preserves the built-in zero-configuration behavior.

Review Party currently invokes all three harnesses directly. ACPX remains a
future transport option rather than part of the current execution path.

Review an exact committed range with full object provenance and an isolated
repository view:

```sh
review-party review bugs --repo . --base HEAD~1 --head HEAD
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
review-party replay rp_... --reviewer opencode \
  --model meta/muse-spark-1.2-contributor --effort high
```

Default replay reuses the exact recorded committed Subject, Profile Revision,
Profile Snapshot, Reviewer/model/effort, capability contract, and execution
deadline. It first proves that the recorded commits still reconstruct the same
Subject identity. Explicit Reviewer/model/effort flags create a newly identified
effective Profile Revision and recorded provenance; Review Party never silently
substitutes an unavailable original choice. Working-changes Reviews are not
replayable. The new record has its own lifecycle, result, attempts, timestamps,
and runtime provenance plus `replays_review_id` linkage to its source. Replay
reproduces experiment inputs, not non-deterministic model output.

Run the packaged general evaluation suite against one explicit Experiment
Configuration without installing Review Party into the shell:

```sh
go run ./cmd/review-party eval run global:general-bugs \
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
Reviewer, model, effort, and deadline. Explicit command flags override that
configuration. Inspect either durable record type afterward:

```sh
go run ./cmd/review-party eval inspect esr_... --format json
go run ./cmd/review-party eval inspect er_... --format json
```

Every Eval Case runs as an ordinary Review over a Git-free Synthetic Review
Subject. Expected Findings and source Git provenance never enter the Review
prompt or execution checkout. Eval execution records completion categories and
leaves semantic adjudication, scoring, and comparison for separate operations.
Invalid result contracts remain Incomplete operational outcomes even if their
raw assistant artifact discusses a plausible bug; semantic scores never infer a
Finding from malformed output.
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
selector; isolate tests and experiments with `XDG_STATE_HOME`.

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

Review Party ships zero-configuration `bugs`, `code-quality`, and
`documentation` Profiles and can load ordinary
Markdown Profiles from a repository or a user-wide library. Packaged Profiles
need no installation and are not copied by `review-party init`. Advanced
callers can create one owned Profile from an explicit starting point:

```sh
review-party profile create security --repo . --blank
review-party profile create docs-team --global --from-packaged documentation
review-party profiles
review-party profile explain security
review-party review security
```

To own the complete packaged starter set, run `review-party profile
install-defaults --repo .` for a deliberately team-shareable repository copy,
or use `--global` for personal copies. Existing files are retained. Owned
Profiles shadow packaged updates, and installation does not change the selected
default Profile.

Repository and global libraries use the same shape:

```text
.reviewparty/
├── config.json
└── profiles/
    ├── bugs.md
    └── security.md
```

The global library is `~/.reviewparty/`. Set `REVIEW_PARTY_HOME` to relocate
it. Profile configuration is optional
and only selects defaults:

```json
{
  "schema": 1,
  "defaultProfile": "security",
  "defaultReviewer": "grok"
}
```

Selection precedence is explicit caller choice, repository config, global
config, then packaged defaults. A repository Markdown file shadows a global or
packaged file with the same name as one complete definition; Review Party does
not concatenate or inherit prompt text. An invalid higher-precedence file stops
before an Agent Harness launches rather than silently selecting another
Profile.

Profile Markdown controls Reviewer Judgment. Review Party still owns tool and
capability restrictions, Context Discovery, immutable Review Subject framing,
the canonical Review Result contract, deadlines, and incomplete-result
semantics. Profiles cannot configure executables, transports, or shell commands.

Packaged Profiles contain their complete purpose-specific judgment instructions.
A repository or global Profile that shadows one of them does not silently inherit
the packaged risk taxonomy, evidence rules, confidence threshold, or review
style. `review-party profile explain PROFILE` shows the authored Markdown and
the compiler-owned execution recipe separately before a Reviewer is launched.
