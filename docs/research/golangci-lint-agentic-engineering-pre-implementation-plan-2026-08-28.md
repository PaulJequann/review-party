# GolangCI-Lint agentic engineering pre-implementation plan

Status: decisions locked; proposed implementation plan awaits acceptance;
not approved for implementation

Last reconciled: 2026-08-28

This document prepares a final implementation plan for a repository-owned
`golangci-lint` policy. It is not the implementation plan. Do not add the
linter, configuration, CI workflow, suppression checks, or source remediations
from this document.

Use this document as the planning ledger. Record new evidence here. Produce the
final implementation plan only after every readiness gate at the end of this
document passes.

## Planning outcome

The final implementation plan must define a lint system that steers agent-written
Go toward maintainable code without rewarding superficial metric fixes. The
system must:

- catch deterministic correctness and error-handling defects;
- keep functions understandable without forcing one-use pass-through helpers;
- enforce Review Party's existing module dependencies;
- keep interfaces small when a smaller interface improves the module;
- expose duplication in production and test code;
- make every lint exception specific, explained, and reviewable;
- support focused local checks and a complete CI check; and
- leave semantic code-quality judgment and delivery authority outside the
  linter.

Lint is the cheap local pre-filter before CodeScene and LLM reviews. It does
not have to be perfect. It has to catch enough deterministic mess that paid
gates see a cleaner diff.

## Planning status vocabulary

- **Confirmed** means current repository evidence or primary documentation
  supports the statement.
- **Provisional** means the direction is plausible but still needs the named
  evidence.
- **Open** means the final implementation plan needs a decision.
- **Blocked** means the next research action needs user approval or unavailable
  tooling.
- **Locked** means the user accepted the decision. Remaining work is evidence
  or implementation, not intention.

## Locked decisions

Accepted 2026-08-28. These replace the earlier unanswered questions of the same
name.

| Decision | Lock |
| --- | --- |
| Lint's job | Cheap local pre-filter before CodeScene and LLM reviews. Defects block. Shape blocks, well enough, not perfect. |
| Shape miss | Loose enough that coherent modules pass. Leftover mess may leak to paid gates. Do not train helper extraction. |
| Local path | `scripts/lint.sh` reads `.golangci-lint-version`, bootstraps `.tools/bin/golangci-lint` if needed, runs focused packages by default, and accepts a full run. |
| Binary | Official v2.13.2 release. SHA256 from the official checksums file. `go.mod` stays untouched. Tarball may cache under `XDG_CACHE_HOME`. The runnable copy lives in `.tools/bin/`. |
| Agent permission | Bootstrap of that pin is approved. Global tool installs and version changes are not. |
| `depguard` | Freeze the live graph, including current `cmd` imports. Do not enforce the 2026-08-10 thin-`cmd` aspiration. |
| `nilnil` | Omit. Keep `stageIntent`'s `nil, nil` no-op. |
| GitHub | Workflow on PRs to `main` runs `./scripts/lint.sh`. A required ruleset check is a follow-up GitHub setting after that job is green. Until then it is not a merge backstop. |
| CI installer | Same `scripts/lint.sh` as local. Checksum the official binary. Do not use `golangci-lint-action`. Pin `actions/checkout` and `actions/setup-go` by SHA. |
| Upgrades | The user authorizes each one. No automated bumps. |
| Plan timing | Write the implementation plan after the baseline, proved `depguard`, and these interfaces. `AGENTS.md` policy text is a later slice and still needs agent-behavior calibration. |
| Tests in CI | Out of this work. |
| Shape thresholds | `cyclop` 20, `gocognit` 25, `funlen` 80/50, `dupl` 120. No blocking `nestif`. Exclude `cyclop`, `gocognit`, and `funlen` from `_test.go`. Keep `dupl` on tests. |
| ST1005 | Disable. Keep branded error strings (`Review Party`, `Eval Run`, and the rest). |
| Adapter JSONL duplication | Extract a shared scanner both Copilot and OpenCode call. Keep adapter-specific `apply*Event` functions. |
| Error contracts | CLI write helper. Close/cleanup helper or `errors.Join` on Close/Remove/Rollback. Fix ignored parse, inspect, persist, and inventory errors. Explained `//nolint:errcheck` only for cobra completion registration and best-effort `Kill`. |
| Suppression form | Only `//nolint:<linter> // explanation`. A small script rejects `//gocognit:ignore`, `//exhaustive:ignore`, `//lint:ignore`, `//nolint:all`, and `#nosec`. |
| `interfacebloat` | max 5 |
| Repeated findings | `max-issues-per-linter: 0`, `max-same-issues: 0`, `uniq-by-line: false` |
| Lint policy files | Agents must not edit them unless the task authorizes a policy change. |

The closest operational equivalent to `node_modules/.bin/eslint` is a committed
version file plus a gitignored project-local binary materialized by
`scripts/lint.sh`. Do not put golangci-lint in `go.mod`. Do not commit the
binary.

Intended layout when implementation is accepted:

```text
.golangci-lint-version         committed pin, currently v2.13.2
.golangci.yml                  committed candidate config after calibration
scripts/lint.sh                committed bootstrap and run wrapper
.tools/bin/golangci-lint       gitignored project-local binary
AGENTS.md                      bootstrap of the pin is approved; version
                               changes are not
```

CI runs `./scripts/lint.sh` so local and hosted runs share the pin, the
checksum, and the suppression auditor.

## Confirmed baseline

### Recursive Go discovery now selects the right code

Commit `2a10ec2` on `origin/main` moved the packaged evaluation corpus to
`internal/engine/testdata/evals`. Go ignores `testdata` during recursive package
discovery, while `go:embed` can still package the corpus.

The following commands passed against the tree at `2a10ec2` on 2026-08-28:

```sh
GOCACHE="$PWD/scratch/go-cache" go list ./...
GOCACHE="$PWD/scratch/go-cache" go test ./... -count=1
```

`go list ./...` found these nine packages:

```text
reviewparty/cmd/review-party
reviewparty/internal/artifact
reviewparty/internal/configuration
reviewparty/internal/engine
reviewparty/internal/model
reviewparty/internal/provenance
reviewparty/internal/result
reviewparty/internal/store
reviewparty/internal/subject
```

Standard `golangci-lint run` package discovery is therefore the candidate CI
interface. The final plan does not need a package-enumeration wrapper. Focused
local runs can pass a package pattern such as `./internal/engine/...`.

Do not add a lint exclusion for `internal/engine/testdata/evals`. The Go package
rules already exclude those fixtures. Their intentionally defective code is
review input, not Review Party source.

### The repository has no lint integration

The tree at `2a10ec2` has no `.golangci` configuration, GitHub Actions workflow,
Makefile, Taskfile, or repository lint script. Adding the tool to `go.mod`
requires a separate dependency approval. That approval was not given. The locked
install path is the official release binary outside `go.mod`.

The module declares Go 1.26. No current Go source contains `//nolint`,
`//lint:ignore`, `//gocognit:ignore`, a Revive disable directive, or `#nosec`.

### Current primary-source facts

- GolangCI-Lint v2 requires `version: "2"` in its configuration. The official
  [configuration reference](https://golangci-lint.run/docs/configuration/file/)
  defines the schema and config verification behavior.
- The official installation page listed
  [v2.13.2](https://golangci-lint.run/docs/welcome/install/local/) when this
  document was reconciled. Recheck the release before implementation.
- Official docs recommend binary installation over `go install`, `go get`,
  `go tool`, and the tools pattern. See
  [local installation](https://golangci-lint.run/docs/welcome/install/local/).
- The official CI guidance recommends an exact linter version because an
  upgrade can add failures. See
  [CI installation](https://golangci-lint.run/docs/welcome/install/ci/).
- The official
  [GitHub Action](https://github.com/golangci/golangci-lint-action) supports an
  exact linter version, a version file (`.golangci-lint-version` or
  `.tool-versions`), and configuration verification. The action was on the v9
  release line when this document was reconciled. Pin the action by commit SHA.
- `depguard` allows only `$gostd` when it has no custom rules. Enabling it
  without repository rules would reject Review Party's valid imports. See the
  official [`depguard` settings](https://golangci-lint.run/docs/linters/configuration/#depguard).
- Go 1.26 support is claimed from golangci-lint v2.9.0. The calibration binary
  v2.13.2 reports `built with go1.27.0`.

### Calibration binary

User approval on 2026-08-28 allowed a scratch-only official binary. It is not
the repository install path.

```text
path: scratch/golangci-lint/golangci-lint
version: 2.13.2
built with: go1.27.0
source: 27774aaf
date: 2026-08-27T23:01:12Z
install: official install.sh into scratch/golangci-lint
```

Do not copy this binary into `.tools/` until the implementation plan is
accepted.

### Live import graph

Production imports, confirmed 2026-08-28:

| Package | Project imports | Third-party |
| --- | --- | --- |
| `cmd/review-party` | `configuration`, `engine`, `model`, `provenance`, `store`, `subject` | `github.com/spf13/cobra`, `github.com/spf13/pflag`, `github.com/mattn/go-isatty` |
| `internal/artifact` | `model` | none |
| `internal/configuration` | none | none |
| `internal/engine` | `artifact`, `configuration`, `model`, `provenance`, `result`, `store`, `subject` | none |
| `internal/model` | none | none |
| `internal/provenance` | `model` | none |
| `internal/result` | `model` | none |
| `internal/store` | `model` | `modernc.org/sqlite` (blank import) |
| `internal/subject` | `model` | none |

No test file adds a project import that production in the same package lacks.
`internal/configuration/plan_integrity_test.go` is `package configuration_test`
and imports `reviewparty/internal/configuration`. That is same-package external
test style, not a cross-package leak. `depguard` may allow that import on
configuration files.

The 2026-08-10 layout note wanted `cmd` to import only `engine` and `model`.
Configuration Hub made `cmd` a real CLI surface. `depguard` freezes the live
graph, not that older aspiration.

## Keep each quality mechanism in its lane

The final system should keep these responsibilities separate:

| Mechanism | Responsibility |
| --- | --- |
| Go compiler, `go test`, and `gofmt` | Language correctness, behavior, and canonical formatting |
| `golangci-lint` | Deterministic error, code-shape, import, duplication, and suppression policy. Cheap local pre-filter. |
| CodeScene | The existing changed-source code-health gate |
| `code-quality` Review Profile | Semantic module depth, ownership, unnecessary concepts, and metric gaming |
| `AGENTS.md` and the caller | Verification scope, exception authority, remediation authority, and delivery decisions |

GolangCI-Lint reports a policy violation. It is not Review Party the product
deciding whether a change may ship. In this repository, a required GitHub lint
check is the merge backstop if the local script was skipped.

## Calibration policy

The following set is the calibration candidate. It is not an approved
`.golangci.yml`. The scratch copy lives at
`scratch/golangci-lint/.golangci.yml`.

### Candidate blocking checks

| Concern | Candidate linters | Calibration settings |
| --- | --- | --- |
| Core correctness | `govet`, `staticcheck`, `ineffassign`, `unused`, `exhaustive`, `durationcheck` | Documented defaults unless the baseline proves a repository need |
| Error handling | `errcheck`, `errorlint`, `nilnesserr` | Trial `errcheck.check-type-assertions: true` and `check-blank: true` |
| SQL ownership | `rowserrcheck`, `sqlclosecheck` | Documented defaults |
| Function shape | `cyclop`, `gocognit`, `funlen`, `dupl` | Start at 12, 15, 80 lines and 50 statements, and 120 tokens. Loosen if a coherent module fails. |
| Interfaces | `interfacebloat`, `iface` | Five methods; only the `identical` analyzer |
| Imports | `depguard` | Live graph, including current `cmd` imports |
| Exceptions | `nolintlint` | Specific linter, explanation, no unused directives |
| Formatting | `gofmt` | v2 formatter check |

Include ordinary `_test.go` files in the first baseline. Do not add blanket test
exclusions for `dupl`, `funlen`, or maintainability checks before seeing real
findings.

### Candidate checks to trial without blocking

- Trial `nestif` at complexity 4. Promote it only if it adds useful early-return
  guidance beyond `gocognit`.
- Trial selected diagnostic `gocritic` checks only if the core set misses a
  demonstrated defect class.
- Trial explicit Revive rules only if each rule enforces an accepted repository
  policy. Do not enable its style-heavy default set as a shortcut.

### Checks to omit

- Omit `cyclop.package-average`. Trivial functions can lower the average, and
  package size changes the meaning of the number.
- Omit `maintidx` until it proves an actionable gap beyond CodeScene and the
  direct shape metrics.
- Omit `nilerr` when `nilnesserr` provides the combined analysis.
- Omit `nilnil`. `stageIntent` returning `nil, nil` for a no-op is an accepted
  contract.
- Omit `bodyclose` and `noctx` until production owns an HTTP client path.
- Omit the package-local `iface` analyzers that may misread exported or
  intentionally opaque interfaces.

## Dependency policy

Use `depguard` to preserve the current deep-module design. Do not invent a
generic domain, repository, and HTTP layer. Do not use lint to thin `cmd`.

Locked allow map:

| Files | Allowed project imports | Allowed third-party |
| --- | --- | --- |
| `internal/model/**` | none | none beyond `$gostd` |
| `internal/configuration/**` | `reviewparty/internal/configuration` for external tests | none beyond `$gostd` |
| `internal/artifact/**`, `internal/provenance/**`, `internal/result/**`, `internal/subject/**` | `reviewparty/internal/model` | none beyond `$gostd` |
| `internal/store/**` | `reviewparty/internal/model` | `modernc.org/sqlite` |
| `internal/engine/**` | `artifact`, `configuration`, `model`, `provenance`, `result`, `store`, `subject` | none beyond `$gostd` |
| `cmd/review-party/**` | `configuration`, `engine`, `model`, `provenance`, `store`, `subject` | `github.com/spf13/cobra`, `github.com/spf13/pflag`, `github.com/mattn/go-isatty` |

A rule that merely reproduces today's graph is ready because today's graph is
the accepted ownership. Later engine splits are a separate architecture change.

Negative fixtures still have to prove that a forbidden import fails with the
expected rule name. That remains research step 4.

## Research plan

### 1. Decide how to provision and pin the tool

Status: **Locked**

Exact linter version: `v2.13.2`.
Local method: official release binary, checksummed, materialized at
`.tools/bin/golangci-lint` by `scripts/lint.sh`.
CI method: the same `scripts/lint.sh`. Pin `actions/checkout` and
`actions/setup-go` by commit SHA. Do not use `golangci-lint-action`.
Proof command after bootstrap: `.tools/bin/golangci-lint version` must report
`2.13.2`.

Calibration used the same version under `scratch/golangci-lint/`. That path is
not the implementation path.

### 2. Produce the real baseline

Status: **Complete**

Scratch config at `scratch/golangci-lint/.golangci.yml` passed
`golangci-lint config verify`. Full-tree run against all nine packages exited
1 with 247 findings after `uniq-by-line: false`. Machine-readable output is
under `scratch/golangci-lint/`. Classification is in Baseline results below.

Cold (empty `GOLANGCI_LINT_CACHE`, existing `GOCACHE`): 16.614s.
Warm: 0.364s, then 0.362s on the full recapture.

A zero exit code did not occur. This is a qualified dirty baseline.

### 3. Calibrate shape rules against maintainable outcomes

Status: **Locked**

Candidate 12 / 15 / 80/50 / 120 on the current tree:

| Linter | Production hits | Test hits | What they are |
| --- | --- | --- | --- |
| `cyclop` 12 | `applyCodexEvent` at 15 | `TestPrepareReplacesObsoleteLedgersInPlace` at 14 | Event-type switch; table-driven test |
| `gocognit` 15 | `inspectRootedPath` at 16, `applyCodexEvent` at 21 | same test at 37 | Rooted path walk; event-type switch |
| `funlen` 80/50 | none | none | Keep |
| `dupl` 120 | Copilot and OpenCode JSONL scan loops | none | Shared scanner primitive, or leave adapters independent |
| `nestif` 4 | same `inspectRootedPath` branch | none | No extra module beyond `gocognit` |

Lowering `gocognit` to 10 adds 12 more functions, several of them coherent
(`copyCapturedTree`, `validateStaged`, `executeEvalCases`). Lowering `cyclop`
to 10 adds `artifact.Publish` at 11. Those are not extraction candidates.

Recommendation for the implementation plan:

- `cyclop` 16 or 20 so `applyCodexEvent` passes
- `gocognit` 22 or 25 so both production hits pass
- keep `funlen` 80/50
- keep `dupl` 120 and treat the adapter scan loop as a real shared helper or
  an explicit adapter-independence exception
- omit blocking `nestif`
- exclude `cyclop`, `gocognit`, and `funlen` from `_test.go` after seeing the
  table-driven test hits. Keep `dupl` on tests.

### 4. Prove the dependency rules

Status: **Complete**

`${base-path}/.../**/*.go` globs matched nothing, so the first "pass" was a
false pass. Working globs are `**/internal/<pkg>/*.go` and
`**/cmd/review-party/*.go`. With those globs the live tree still has zero
`depguard` findings.

Nested-module fixtures under `scratch/golangci-lint/depguard-negatives/` fail
with the expected rule name:

| Tag | File | Rule |
| --- | --- | --- |
| `depguard_model` | `internal/model/bad.go` | `model` |
| `depguard_configuration` | `internal/configuration/bad.go` | `configuration` |
| `depguard_leaf` | `internal/artifact/bad.go` | `leaf-model-only` |
| `depguard_store` | `internal/store/bad.go` | `store` |
| `depguard_engine` | `internal/engine/bad.go` | `engine` |
| `depguard_cmd` | `cmd/review-party/bad.go` | `cmd` |

Untagged fixtures are clean.

### 5. Decide the error and suppression contracts

Status: **Locked**

Error classes and contracts are in Locked decisions. Probe under
`scratch/golangci-lint/suppression-probe/` showed:

- `//nolint:unused // reason` silences `unused` and satisfies `nolintlint`.
- `//nolint:all` and bare `//nolint` silence linters. `nolintlint` then fails
  them for missing specificity or explanation. Still reject them in the
  auditor so agents do not learn that form.
- `//gocognit:ignore` silences `gocognit` and is invisible to `nolintlint`.
- `//exhaustive:ignore` silences `exhaustive` and is invisible to `nolintlint`.
- `//lint:ignore U1000` silences `unused` and is invisible to `nolintlint`.
- `#nosec` does not silence `errcheck`.

So `nolintlint` is not enough. The repository needs a small directive auditor.

### 6. Decide the local and CI interfaces

Status: **Locked**

Local: `./scripts/lint.sh` with optional package patterns.
CI: the same script on pull requests to `main`. Tests stay out of this
workflow. A GitHub ruleset that requires the check is a follow-up setting
after the job is green.

### 7. Write and test the agent policy

Status: **Open, implementation slice after the plan**

Draft a compact `AGENTS.md` addition that:

- treats a shape finding as design evidence, not a prompt to extract helpers;
- names `./scripts/lint.sh` as the only lint entrypoint;
- pre-approves bootstrap of the pinned binary;
- forbids global tool installs and version changes.

Do not treat a draft as done until calibration cases show the intended agent
behavior.

### 8. Produce the final implementation plan

Status: **Proposed.** See
[`docs/golangci-lint-implementation-plan.md`](../golangci-lint-implementation-plan.md).
Do not start a slice until the user accepts that plan.

Create the final implementation plan only after the remaining evidence below
is closed. The final plan must:

- name every file to add or change;
- order baseline remediation, configuration, CI, and agent-policy work into
  reviewable slices;
- give each slice focused tests and acceptance checks;
- state the exact pinned versions and selected linter settings;
- include the proved `depguard` rules;
- preserve the existing CodeScene and Review Profile responsibilities;
- identify any source changes that require CodeScene scoring; and
- define the evidence required before the lint job becomes blocking.

Mark this document as superseded and link to the accepted plan. Do not begin an
implementation slice until the user accepts the final plan.

## Baseline results

Recorded 2026-08-28 against HEAD `426748e` plus this research-doc edit, using
scratch golangci-lint v2.13.2 and `scratch/golangci-lint/.golangci.yml`.

```text
config verify: pass
packages: ./... (nine packages)
issues: 247
exit: 1
cold: 16.614s
warm: 0.364s
artifacts:
  scratch/golangci-lint/.golangci.yml
  scratch/golangci-lint/baseline.json
  scratch/golangci-lint/baseline.txt
  scratch/golangci-lint/baseline-inventory.tsv
```

Enabled linters with zero findings: `ineffassign`, `durationcheck`,
`nilnesserr`, `rowserrcheck`, `sqlclosecheck`, `funlen`, `interfacebloat`,
`iface`, `depguard`, `nolintlint`, and formatter `gofmt`.

| Linter | Count | Prod | Test |
| --- | --- | --- | --- |
| `errcheck` | 211 | 185 | 26 |
| `unused` | 11 | 10 | 1 |
| `staticcheck` | 7 | 7 | 0 |
| `exhaustive` | 6 | 6 | 0 |
| `gocognit` | 3 | 2 | 1 |
| `govet` | 3 | 0 | 3 |
| `cyclop` | 2 | 1 | 1 |
| `dupl` | 2 | 2 | 0 |
| `errorlint` | 1 | 1 | 0 |
| `nestif` | 1 | 1 | 0 |

### Shape and duplication

- `inspectRootedPath` (`gocognit` 16, `nestif` 4) is a rooted symlink-safe
  walk. Splitting it would hide the invariant. Leave it. Omit blocking
  `nestif`.
- `applyCodexEvent` (`gocognit` 21, `cyclop` 15) is an event-type switch.
  Extracting one-arm helpers would be metric theater. Raise the limits.
- `TestPrepareReplacesObsoleteLedgersInPlace` is a table-driven ledger test
  (`gocognit` 37, `cyclop` 14, plus `govet` copylocks). Exclude test files
  from `gocognit`/`cyclop`/`funlen`. Fix the copylocks by taking `*sql.Tx`.
- Copilot and OpenCode share a JSONL scan loop (`dupl`). A shared scanner that
  both adapters call would be a real primitive. Do not extract a
  one-use wrapper. Keep `dupl` at 120.

### Dead code (`unused`)

Leftover constructors and aliases: `newConductor`,
`newConductorWithCatalog`, `configureReviewerCatalog`,
`applyDefaultReviewer`, `validateEffectiveDefault`, `listOrNone`,
`executableProfileNames`, unused `path` field, `scriptedExecutor.lastAttempt`,
and compatibility aliases `maxResultSize` and
`currentReviewRecordSchemaVersion`. Pre-release policy is to delete with
path-specific approval, not keep shims.

### Deterministic defects

- `errorlint` on `internal/subject/numstat.go`: `err.(*exec.ExitError)` should
  be `errors.As`.
- `exhaustive` on six switches. `sourceFor` uses `default` for global scope
  instead of `case ScopeGlobal`. The eval counters omit non-terminal states.
  Make the accepted cases explicit.
- `govet` copylocks in `ledger_test.go`: seed funcs take `sql.Tx` by value.
- `staticcheck` S1016 in `standard_commands.go`: convert
  `libraryListOptions` to `partiesOptions` instead of a field-by-field
  literal.
- `staticcheck` ST1005 on six error strings that capitalize domain terms
  (`Review Party`, `Profile`, `Eval Run`, `Concurrency Limit`). This is a
  product-language clash with Go's error-string rule, not a shape metric.
  Decide to uncapitalize, keep branded names, or disable ST1005.

### `errcheck` classes

`check-blank: true` is in force. `_ = x.Close()` still fails.

| Class | Count | Disposition |
| --- | --- | --- |
| `fmt.Fprintf` / `Fprintln` / `Fprint` / `io.WriteString` / `destination.Write` | 128 | CLI writes. One helper that returns the write error to the command, not 128 `//nolint`s. |
| `Close` on files, roots, rows, db, ledger, store, checkout | 38 | Named closer that returns or joins the error. Bare `defer x.Close()` is the bug. |
| `os.Remove`, `RemoveAll`, `root.Remove`, `root.RemoveAll` | 13 | Cleanup must surface or join the error. |
| `tx.Rollback` | 5 | Join rollback errors on the failure path. |
| `cmd.RegisterFlagCompletionFunc` | 3 | Already `_ =`. Best-effort cobra API. Tiny explained `//nolint:errcheck` or a named ignore helper. |
| `time.ParseDuration` | 3 | `eval_retry.go` ignores parse failure and can get a zero backoff. Fix. |
| `syscall.Kill` | 2 | Best-effort process-group signals in `process_group_unix.go` (`_ = Kill`). Named best-effort helper or explained `//nolint`. `processAlive` already uses the error. |
| `conductor.InspectEvalRun` / `Inspect` | 4 | Tests and one inspect path dropping errors. Check them. |
| `item.Name` | 1 | `cmd/review-party/config_review_mutations.go` uses `name, _ := item.Name()`. Other call sites already check. Fix. |
| `manager.PartyInventory` / `ProfileInventory` | 2 | Inventory errors ignored in cmd. Fix. |
| `publisher.store.Remove` | 1 | Artifact cleanup error ignored. Fix. |
| `conductor.absorbBundleMember` | 1 | `party.go` drops the persist error with `_`. Fix. |
| `filepath.WalkDir` | 1 | Test walk error ignored. Fix. |
| type assertion `ctx.Value` in `attemptGateFromContext` | 1 | `check-type-assertions: true`. Missing gate is a real no-gate. Use comma-ok and return nil when `!ok`. |
| `profileDefinitionFor` in `summaryProfile` | 1 | Ignored error at `library.go:109`. Check it. |

No global function-name exclusion. The CLI writer and closer helpers are
shared contracts, not silencers.

### Runtime

Local full-tree cold is about 17s. Warm is under half a second. That is cheap
enough for the pre-PR script and for focused agent runs. CI duration is still
unmeasured.

### Remediation size

Most of the 247 findings collapse into a few contracts: CLI writes, closes,
cleanups, then a short list of real defects and unused symbols. Shape is not
the bulk. Required CI should land after those remediations, not on a red
baseline.

## Remaining evidence

Intention questions are closed. The proposed implementation plan still needs
user acceptance. CI duration still needs a trial workflow after that plan is
accepted.

## Final-plan readiness gate

Do not call this planning work complete until all of these statements are true:

- [x] The user approved the tool dependency used for calibration.
- [x] The selected v2 configuration passes `golangci-lint config verify`.
- [x] The complete baseline is recorded and every finding is classified.
- [x] Every blocking linter and threshold has repository evidence. Thresholds
      are locked in this ledger and copied into the proposed plan.
- [x] The `depguard` rules pass the current tree and fail the negative cases.
- [x] The error-handling policy accounts for every ignored result.
- [x] The suppression policy covers the enabled linters' alternate directives.
- [x] The local and hosted commands use the same configuration and pinned
      linter version at the intention level.
- [x] The hosted gate is accepted at the intention level: PR workflow runs
      `./scripts/lint.sh`. Ruleset-required is a follow-up GitHub setting.
      Local run time is measured. CI duration is not.
- [ ] The `AGENTS.md` text passes the agent-behavior calibration.
- [x] The final implementation slices have exact files, dependencies,
      verification commands, and completion criteria. See the proposed plan.
- [x] The user accepted the resulting implementation plan.

## Evidence log

### 2026-08-28

- Confirmed live `origin/main` at `426748e` (`docs: add golangci-lint
  pre-implementation plan`). The worktree has uncommitted edits to this file.
- Confirmed that the packaged corpus lives under
  `internal/engine/testdata/evals`.
- Confirmed that `go list ./...` finds nine real packages.
- Confirmed that `go test ./... -count=1` passes against the tree at `2a10ec2`.
- Confirmed that the repository has no lint configuration or hosted workflow.
- Confirmed that the source tree contains none of the suppression forms listed
  in the current inventory.
- Locked the decision tree in-session. See Locked decisions.
- Installed official golangci-lint v2.13.2 into `scratch/golangci-lint/` with
  `https://golangci-lint.run/install.sh`. `golangci-lint version` reported
  `2.13.2` built with go1.27.0 from `27774aaf` on 2026-08-27T23:01:12Z.
- Latest stable at reconcile time was v2.13.2, not v2.13.1.
- Confirmed the live production import graph and the absence of extra
  test-only project imports.
- `golangci-lint config verify -c scratch/golangci-lint/.golangci.yml` passed.
- Full-tree candidate run: 247 issues, exit 1, cold 16.614s, warm 0.364s.
  Default `uniq-by-line: true` hid overlapping `cyclop` hits on the same
  functions as `gocognit`. The recorded baseline uses `uniq-by-line: false`.
- `depguard` produced zero findings on the current tree.
- `${base-path}/**/*.go` globs matched no files. Corrected globs
  `**/internal/<pkg>/*.go` still pass the live tree and fail the six nested
  negative fixtures under `scratch/golangci-lint/depguard-negatives/`.
- Suppression probe under `scratch/golangci-lint/suppression-probe/` showed
  `//gocognit:ignore`, `//exhaustive:ignore`, and `//lint:ignore U1000`
  silence linters without `nolintlint`.
- User accepted the remaining recommendations: shape 20/25/80/50/120, disable
  ST1005, shared JSONL scanner, error-contract helpers, and a suppression
  auditor. Proposed plan is
  [`docs/golangci-lint-implementation-plan.md`](../golangci-lint-implementation-plan.md).
- Pinned `actions/checkout` `v6.1.0` to commit
  `d23441a48e516b6c34aea4fa41551a30e30af803` and `actions/setup-go` `v6.5.0`
  to commit `924ae3a1cded613372ab5595356fb5720e22ba16` with
  `git ls-remote` against each release tag.
- GitHub Actions duration is still unmeasured from this machine. Record cold
  and warm CI duration after the first green workflow run.
