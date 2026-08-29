# GolangCI-Lint implementation plan

Status: accepted 2026-08-28; slices 1-6 complete

Last reconciled: 2026-08-28

Research ledger:
[`research/golangci-lint-agentic-engineering-pre-implementation-plan-2026-08-28.md`](research/golangci-lint-agentic-engineering-pre-implementation-plan-2026-08-28.md).

This plan is the implementation document. Accepting it authorizes the slices
below, including the unused-symbol deletions listed in slice 1. It does not
authorize any other deletions.

## Outcome

Agents run a cheap local lint script before CodeScene and LLM reviews. The
same script runs on pull requests to `main`. A GitHub ruleset that requires
that check is a follow-up setting the user applies after the workflow is
green. Until then the job can fail red and still merge. Shape rules stay
loose enough that coherent modules pass. The linter reports policy
violations. It does not own delivery.

## Locked settings

| Item | Value |
| --- | --- |
| Linter | official binary v2.13.2, not in `go.mod` |
| Local | `./scripts/lint.sh` [packages...] |
| Binary path | `.tools/bin/golangci-lint` (gitignored) |
| Version file | `.golangci-lint-version` containing `v2.13.2` |
| Tarball cache | `${XDG_CACHE_HOME:-$HOME/.cache}/review-party/tools/` |
| CI | `./scripts/lint.sh` on pull requests to `main`. Pin `actions/checkout` and `actions/setup-go` by SHA. Do not use `golangci-lint-action`. |
| `cyclop` | 20 |
| `gocognit` | 25 |
| `funlen` | 80 lines, 50 statements |
| `dupl` | 120, including tests |
| `interfacebloat` | max 5 |
| `nestif` | omitted |
| Test shape | exclude `cyclop`, `gocognit`, `funlen` from `_test.go`. Keep `dupl` on tests. |
| Lint policy files | agents must not edit them unless the task says so |
| ST1005 | disabled |
| `nilnil` | omitted |
| Allowed suppression | `//nolint:<linter> // explanation` only |
| Upgrades | user-authorized, no bots |

`depguard` freezes the live graph. File globs must be `**/internal/<pkg>/*.go`
and `**/cmd/review-party/*.go`. `${base-path}/**/*.go` matches nothing.

## Files

Add:

```text
.golangci-lint-version
.golangci.yml
scripts/lint.sh
scripts/verify-lint-suppressions.sh
.github/workflows/lint.yml
```

Change:

```text
.gitignore                         add /.tools/
AGENTS.md                          bootstrap exception, lint entrypoint, shape policy
cmd/review-party/*.go              CLI write helper
internal/engine/copilot.go
internal/engine/opencode.go        shared JSONL scanner
other Go files listed per slice
```

Do not add golangci-lint to `go.mod`. Do not commit `.tools/`.

## Slice 1. Defects and unused symbols (complete)

Remove unused leftovers and fix the deterministic findings that are not the
CLI-write or Close/Remove bulk.

Unused symbols. Accepting this plan approves deleting these symbols. If a file
has no remaining content, delete the file too:

- `internal/engine/conductor.go`: `newConductor`, `newConductorWithCatalog`
- `internal/engine/conductor_test.go`: `(*scriptedExecutor).lastAttempt`
- `internal/engine/configuration.go`: `configureReviewerCatalog`,
  `applyDefaultReviewer`, `validateEffectiveDefault` if still unreferenced
- `internal/engine/diagnostics.go`: `listOrNone`, and the file if emptied
- `internal/engine/library.go`: unused `path` field on `resolvedProfile` if
  nothing reads it
- `internal/engine/profile_resolution.go`: `executableProfileNames`
- `internal/result/model_aliases.go`: `maxResultSize`
- `internal/store/model_aliases.go`: `currentReviewRecordSchemaVersion`

Fixes:

- `internal/subject/numstat.go`: `errors.As` instead of `err.(*exec.ExitError)`
- six `exhaustive` switches: list accepted cases explicitly, including
  `ScopeGlobal` in `sourceFor`
- `internal/store/ledger_test.go`: seed funcs take `*sql.Tx`
- `cmd/review-party/standard_commands.go`: S1016 conversion
- ignored errors that are real: `time.ParseDuration` in `eval_retry.go`,
  `Inspect` / `InspectEvalRun`, `item.Name`, profile/party inventory,
  `publisher.store.Remove`, `absorbBundleMember`, `filepath.WalkDir`,
  `profileDefinitionFor` in `summaryProfile`, comma-ok in
  `attemptGateFromContext`

Do not rewrite branded ST1005 strings. Slice 4 disables that check.

Verification: focused `go test` on touched packages, `gofmt` on touched files,
CodeScene on touched source. No lint config yet.

## Slice 2. Error contracts (complete)

Add a `cmd` helper that returns write errors to the command instead of 128
unchecked `fmt.Fprint*` calls.

On Close, Remove, RemoveAll, and Rollback, join the error into the function
result. Package-local helpers are fine. Do not add a new `internal` package
just for lint.

The only explained `//nolint:errcheck` lines allowed in this slice:

- `cmd.RegisterFlagCompletionFunc` (three sites; cobra best-effort)
- `syscall.Kill` in `internal/engine/process_group_unix.go` (best-effort
  process-group signal)

Verification: same as slice 1, plus a focused test that a failed CLI write
becomes a non-zero command result if one does not already exist.

## Slice 3. Shared JSONL scanner (complete)

Extract the duplicated Copilot/OpenCode scan loop in `internal/engine` into one
helper both adapters call. Keep `applyCopilotEvent` and `applyOpenCodeEvent`
adapter-specific.

Verification: existing adapter tests, CodeScene on the touched engine files.

## Slice 4. Repository lint toolchain (complete)

Land the config, pin, scripts, gitignore, and the standing AGENTS.md bootstrap
exception. Do this only after slices 1-3 leave `golangci-lint run` clean
against the new config.

`.golangci.yml` (v2, `default: none`):

- enable: `govet`, `staticcheck`, `ineffassign`, `unused`, `exhaustive`,
  `durationcheck`, `errcheck`, `errorlint`, `nilnesserr`, `rowserrcheck`,
  `sqlclosecheck`, `cyclop`, `gocognit`, `funlen`, `dupl`, `interfacebloat`,
  `iface`, `depguard`, `nolintlint`
- `errcheck.check-blank` and `check-type-assertions`: true
- `staticcheck.checks`: all except `-ST1005`
- `iface.enable`: `identical` only
- `interfacebloat.max`: 5
- `nolintlint`: require specific linter, require explanation, reject unused
- `depguard` live-graph rules with the proved `**/.../*.go` globs
- exclude `cyclop`, `gocognit`, `funlen` on `_test.go`
- `issues.max-issues-per-linter`: 0, `max-same-issues`: 0, `uniq-by-line`: false
- formatter: `gofmt`
- no `std-error-handling` preset

`scripts/lint.sh`:

1. Read `.golangci-lint-version`.
2. Cache directory is `${XDG_CACHE_HOME:-$HOME/.cache}/review-party/tools`.
3. If `.tools/bin/golangci-lint` is missing or the wrong version, download the
   official linux/darwin amd64/arm64 tarball into a temporary file, verify
   SHA256 against the official checksums file (`sha256sum` on Linux,
   `shasum -a 256` on macOS), then atomically move the tarball into the cache
   and atomically install the binary into `.tools/bin/`. If another process
   wins the race, verify the installed version and continue.
4. Run `scripts/verify-lint-suppressions.sh`.
5. Run `.tools/bin/golangci-lint run` with remaining argv as package patterns,
   defaulting to `./...`.

`scripts/verify-lint-suppressions.sh` fails on:

```text
//gocognit:ignore
//gocyclo:ignore
//exhaustive:ignore
//lint:ignore
//lint:file-ignore
//nolint:all
#nosec
```

and on `//nolint` that is not `//nolint:<name>`. `nolintlint` still owns
explanation and unused-directive checks.

AGENTS.md in this slice only: repository-pinned tools from `scripts/` may be
bootstrapped; `./scripts/lint.sh` is the lint entrypoint; do not install or
bump tools globally. Do not edit lint policy files unless the task
explicitly authorizes a policy change. That includes `.golangci.yml`,
`.golangci-lint-version`, `scripts/lint.sh`,
`scripts/verify-lint-suppressions.sh`, the lint workflow, and `depguard`
rules.

Verification:

```sh
./scratch/golangci-lint/golangci-lint config verify -c .golangci.yml
./scripts/lint.sh
./scripts/lint.sh ./internal/engine/...
```

Full-tree `./scripts/lint.sh` must exit 0. Warm local runtime should stay well
under 5s on this tree.

## Slice 5. GitHub Actions job (complete)

Add `.github/workflows/lint.yml` that runs the same script as developers:

- `pull_request` to `main`
- `permissions.contents: read`
- pin `actions/checkout` and `actions/setup-go` to full 40-character commit
  SHAs with version comments. Go version comes from `go.mod`.
- run `./scripts/lint.sh` with no extra args (full tree)
- do not use `golangci/golangci-lint-action`

That keeps checksum verification, suppression auditing, and the linter pin on
one path. Resolve action SHAs at implementation with `git ls-remote`. Do not
use floating major tags.

After the first green run, record cold and warm CI duration in the research
ledger. CI duration is still unmeasured from this machine.

The user must mark `lint` required in a GitHub ruleset after the first green
run. That setting is outside the repo. Do not call CI required until that
ruleset exists.

## Slice 6. Agent shape policy (complete)

Add the compact AGENTS.md rule: a shape finding is design evidence. Improve the
owning module or write a specific explained `//nolint`. Do not extract one-use
pass-through helpers, widen an interface, or scatter one module's knowledge.

Also restate the slice 4 policy-file rule: a lint failure is not permission to
weaken `.golangci.yml`, raise thresholds, add path exclusions, skip the
auditor, or edit `depguard`.

Calibrate against the recorded cases: `inspectRootedPath` and `applyCodexEvent`
must stay unshredded; the JSONL scanner extraction in slice 3 is the positive
example.

## CodeScene

Score every touched or new Go source file in each slice. New files at least
9.0. No regressions. `pre_commit_code_health_safeguard` once before the local
commit of that slice.

## Out of scope

- `go test` in CI
- `bodyclose`, `noctx`, `nilnil`, `nestif`, `maintidx`, Revive defaults
- automated linter upgrades
- thinning `cmd` back to `engine`+`model` only
- changing `stageIntent`'s `nil, nil` no-op
