# Filesystem-backed Review Profiles V1

Status: implemented

## Decision

Review Party resolves named Review Profiles through one deep profile-library
Module inside the Conductor. The Module hides repository/personal discovery,
strict configuration parsing, whole-file precedence, Markdown validation,
Reviewer defaults, prompt assembly, Profile Revision hashing, and source
provenance.

The caller continues to select only a repository, Review Subject, optional
Profile name, and optional Reviewer. Agent Harness adapters receive one compiled
prompt and do not learn filesystem conventions.

## Filesystem contract

Repository scope:

```text
<git-root>/.reviewparty/
├── config.json
└── profiles/<name>.md
```

Personal scope:

```text
${XDG_CONFIG_HOME:-$HOME/.config}/review-party/
├── config.json
└── profiles/<name>.md
```

Packaged defaults use the same Markdown representation and are embedded in the
binary. Personal and Repository `config.json` documents share schema version 1
and use `schema_version`, `defaults.profile`, `defaults.reviewer`, and
`reviewers.<id>.enabled|model|allowed_models`. Repository reviewer values win
per field; fields absent there may still come from Personal Configuration.
Document validation enforces shape, scope, and known Reviewer names in
isolation. Repository scope cannot author Personal-only state or Eval settings.
Unknown fields, unsupported schemas, unsafe names, invalid UTF-8, empty Profiles,
oversized files, symlinks, and special files fail before launch.

## Effective policy and publication contract

`Manager.Resolve` and `Manager.Plan` resolve `enabled`, `model`, and
`allowed_models` independently across Repository, Personal, and packaged
values. They validate model and allowlist consistency only after that per-field
precedence produces the final effective Reviewer policy.

An enabled Reviewer with a non-empty packaged model uses that model when no
authored model wins. If an authored `allowed_models` restriction also wins, the
allowlist must contain the packaged model. To clear the restriction, omit the
`allowed_models` field or use `SetReviewerAllowedModels{Models: nil}`. An empty
array means no model is allowed; it does not clear the restriction. If the final
effective Reviewer is disabled, its model and allowlist are inert and cannot
make the policy invalid.

`Manager.Plan` returns an opaque plan. Its preview accessors return defensive
copies, so a caller cannot mutate the staged documents through `Changes`,
`Scopes`, or `Paths`. Planning also captures the baseline bytes for each target
file. `Manager.Publish` preflights every target against its baseline and rejects
a stale plan before any write begins. After a concurrent configuration edit,
the caller must create and review a fresh plan before publishing.

## Resolution

Profile and Reviewer selection precedence is:

1. explicit caller selection;
2. repository config;
3. personal config;
4. packaged/program defaults.

A named Profile is searched in repository, personal, then packaged scope. The
first existing file wins as a whole definition. There is no inheritance,
fragment concatenation, environment interpolation, remote include, or script
execution. A malformed higher-precedence file is an error, never permission to
fall through.

Review Party compiles user-authored judgment instructions together with its
owned capability restrictions, Context Discovery, canonical Review Result
contract, and immutable Review Subject. Actual normalized Markdown bytes,
Reviewer provenance, Pass plan, compiler revision, and result-contract revision
contribute to the Profile Revision. The Review Record preserves stable source
provenance, the source digest, and the normalized authored instruction snapshot.

Profile Markdown is the whole purpose-specific Reviewer Judgment definition.
The compiler does not silently append a packaged Profile's risk taxonomy,
materiality guidance, evidence heuristics, confidence threshold, or review
style after a repository or global Profile shadows it. Compiler-owned additions
remain profile-neutral Review Party constraints and result framing.

## Operations

- `review-party init [--repo PATH] [--state-dir PATH] [--config PATH]` prepares
  managed Review Record state selected by that configuration and does not
  create Profile material.
- `review-party profile create NAME (--blank|--from-packaged PROFILE)` creates
  exactly one owned Profile at repository or personal scope without overwriting.
- `review-party profile install-defaults [--repo PATH|--global]` creates owned
  copies of every packaged starter Profile in Repository or Personal
  Configuration (the existing `--global` spelling selects Personal), retains
  existing files, and does not change default Profile selection.
- `review-party profiles [--repo PATH]` lists effective named Profiles and
  their winning sources; invalid peer files are included with validation errors
  without hiding valid Profiles.
- `review-party profile explain PROFILE` shows the winning source, Reviewer,
  revision, Pass plan, and authored Markdown without launching a harness.
- `review-party review [PROFILE]` uses layered defaults when the Profile or
  Reviewer is omitted.

## Non-goals

- Profile inheritance or reusable prompt fragments.
- User-defined Pass graphs, result contracts, capabilities, commands, models,
  harnesses, or transports.
- Treating Profile text as project delivery authority or remediation approval.
- Silent Reviewer or Profile substitution.

## Acceptance evidence

Focused tests deliberately prove repository shadowing, personal defaults,
pre-launch rejection without fallback, searched-location diagnostics, strict
config validation, content-sensitive revisions, non-destructive initialization,
the packaged zero-config path, and the `init` to `profiles` CLI journey.
