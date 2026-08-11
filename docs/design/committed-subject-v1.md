# Committed Subject execution v1

Status: implemented locally on 2026-08-11.

## Interface

`SubjectReference` distinguishes `working-changes` from `committed-range`; a
committed range requires both base and head inputs. Subject resolution owns all
Git revision and diff commands and returns full commit object IDs, the
binary-capable patch, changed paths, size facts, and a deterministic identity.
Domain values contain no Git commands or mutable revision semantics.

`subject.PrepareExecution` is the small execution Interface. A caller supplies
one frozen Subject and ownership label and receives the exact repository path
for the Reviewer plus an idempotent `Close`. The Module hides detached-worktree
creation, external ownership metadata, exact-path and Git-registration
validation, cleanup, and inactive-leftover reconciliation. Working changes use
the caller repository without pretending to provide isolation.

## Runtime and ownership

Committed execution checkouts live beneath the Review Party-owned directory
`$TMPDIR/review-party-worktrees` (or the platform temporary directory). Each is
detached at the recorded full head object ID. A sibling `*.owner.json` file,
outside the checkout, records only repository path, exact checkout path, and
owning process ID. It contains no credentials.

Cleanup runs after the Reviewer executor returns and its process tree has been
reaped. It accepts only a direct child of the owned root whose sibling metadata
matches and whose path is registered by Git for the recorded repository. Git
performs `worktree remove --force`; Review Party does not recursively delete an
unresolved path. A cleanup failure is returned with the persisted Review ID and
does not rewrite a valid Result.

Preparation reconciles at most 100 ownership records. Live owning processes are
left alone. Only inactive, metadata-backed, registered paths beneath the owned
root are removed; unrelated Git worktrees are not candidates. This bounded
reconciliation is crash recovery, not the normal destructor.

## Source-view and environment policy

The checkout is a version-isolated source view, not a dependency-provisioned or
security sandbox. Review Party copies no caller files and invokes no package
manager, so ignored dependency trees, untracked `.env` files, registry files,
and credentials remain absent. Committed files remain visible because they are
part of the frozen Subject.

Every direct Reviewer adapter replaces unrestricted environment inheritance
with an explicit allowlist. Common process/network entries are `PATH`, `HOME`,
`TMPDIR`, locale/terminal variables, `NO_COLOR`, XDG directories, proxy
variables, and TLS certificate paths. Adapter additions are:

- Grok: `GROK_API_KEY`, `XAI_API_KEY`.
- OpenCode: `OPENCODE_CONFIG` and the explicitly supported provider API-key
  names.
- Copilot: `GH_TOKEN`, `GITHUB_TOKEN`.

OpenCode's generated capability configuration is appended explicitly. Review
Party records Reviewer/model/harness/transport provenance, not environment
values. Dependency installation, environment-file injection, network fetching,
and reusable worktree pools remain future capabilities.
