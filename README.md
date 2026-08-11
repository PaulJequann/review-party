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

List and explain the built-in Review Profiles without launching an Agent
Harness or creating a Review Record:

```sh
review-party profiles
review-party explain bugs
review-party explain documentation --reviewer copilot
review-party config path
review-party config show
```

Run a bug or Documentation Review with an explicitly selected direct adapter:

```sh
review-party review bugs --reviewer grok
review-party review documentation --reviewer opencode \
  --model opencode-go/deepseek-v4-flash \
  --effort high
```

`bugs` remains the default Profile, and Grok is the default Reviewer. Both
Profiles require the same repository read/search capability contract with
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

## User configuration

Review Party loads user policy from
`${XDG_CONFIG_HOME:-$HOME/.config}/review-party/config.json`. The versioned JSON
document may set one Default Reviewer and enable, disable, select, or restrict
models for each supported Reviewer:

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

## Operational Review Records

New Review Records use schema version 2 and retain machine-readable operational
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
diagnostic string. Existing unversioned filesystem records remain inspectable
as legacy schema-v1 records and retain their original `incomplete_cause`.
Earlier canonical-v1 count/raw-only results remain inspectable as well.

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

Review Party ships a zero-configuration `bugs` Profile and can load ordinary
Markdown Profiles from a repository or a user-wide library. Initialize a
repository library and create another Profile by adding a Markdown file:

```sh
review-party init
$EDITOR .reviewparty/profiles/security.md
review-party profiles
review-party profile explain security
review-party review security
```

Repository and global libraries use the same shape:

```text
.reviewparty/
├── config.json
└── profiles/
    ├── bugs.md
    └── security.md
```

The global library is `~/.reviewparty/`. Set `REVIEW_PARTY_HOME` to relocate
it, or run `review-party init --global` to create it. Configuration is optional
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
