# Filesystem-backed Review Profiles V1

Status: implemented

## Decision

Review Party resolves named Review Profiles through one deep profile-library
Module inside the Conductor. The Module hides repository/global discovery,
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

Global scope:

```text
~/.reviewparty/
├── config.json
└── profiles/<name>.md
```

`REVIEW_PARTY_HOME` relocates the global directory. Packaged defaults use the
same Markdown representation and are embedded in the binary.

The optional config has schema 1 and only `defaultProfile` and
`defaultReviewer`. Unknown fields, unsupported schemas, unsafe names, invalid
UTF-8, empty Profiles, oversized files, symlinks, and special files fail before
launch.

## Resolution

Profile and Reviewer selection precedence is:

1. explicit caller selection;
2. repository config;
3. global config;
4. packaged/program defaults.

A named Profile is searched in repository, global, then packaged scope. The
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

## Operations

- `review-party init [--repo PATH]` creates repository starter files without
  overwriting existing paths.
- `review-party init --global` creates the global starter library.
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

Focused tests deliberately prove repository shadowing, global defaults,
pre-launch rejection without fallback, searched-location diagnostics, strict
config validation, content-sensitive revisions, non-destructive initialization,
the packaged zero-config path, and the `init` to `profiles` CLI journey.
