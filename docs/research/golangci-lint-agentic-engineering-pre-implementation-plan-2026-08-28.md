# GolangCI-Lint agentic engineering pre-implementation plan

Status: research and decisions in progress; not approved for implementation

Last reconciled: 2026-08-28

This document prepares a final implementation plan for a repository-owned
`golangci-lint` policy. It is not the implementation plan. Do not add the
linter, configuration, CI workflow, suppression checks, or source remediations
from this document.

Use this document as the planning ledger. Record new evidence here, resolve the
open questions, and replace provisional choices with decisions. Produce the
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

## Planning status vocabulary

- **Confirmed** means current repository evidence or primary documentation
  supports the statement.
- **Provisional** means the direction is plausible but still needs the named
  evidence.
- **Open** means the final implementation plan needs a decision.
- **Blocked** means the next research action needs user approval or unavailable
  tooling.

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
Makefile, Taskfile, or repository lint script. The `golangci-lint` binary is not
available on `PATH`. Adding or installing it requires explicit dependency
approval under `AGENTS.md`.

The module declares Go 1.26. No current Go source contains `//nolint`,
`//lint:ignore`, `//gocognit:ignore`, a Revive disable directive, or `#nosec`.

### Current primary-source facts

- GolangCI-Lint v2 requires `version: "2"` in its configuration. The official
  [configuration reference](https://golangci-lint.run/docs/configuration/file/)
  defines the schema and config verification behavior.
- The official installation page listed
  [v2.13.1](https://golangci-lint.run/docs/welcome/install/local/) when this
  document was reconciled. Recheck the release before the final plan because
  this fact changes over time.
- The official CI guidance recommends an exact linter version because an
  upgrade can add failures. See
  [CI installation](https://golangci-lint.run/docs/welcome/install/ci/).
- The official
  [GitHub Action](https://github.com/golangci/golangci-lint-action) supports an
  exact linter version, a version file, and configuration verification. The
  action was on the v9 release line when this document was reconciled.
- `depguard` allows only `$gostd` when it has no custom rules. Enabling it
  without repository rules would reject Review Party's valid imports. See the
  official [`depguard` settings](https://golangci-lint.run/docs/linters/configuration/#depguard).

## Keep each quality mechanism in its lane

The final system should keep these responsibilities separate:

| Mechanism | Responsibility |
| --- | --- |
| Go compiler, `go test`, and `gofmt` | Language correctness, behavior, and canonical formatting |
| `golangci-lint` | Deterministic error, code-shape, import, duplication, and suppression policy |
| CodeScene | The existing changed-source code-health gate |
| `code-quality` Review Profile | Semantic module depth, ownership, unnecessary concepts, and metric gaming |
| `AGENTS.md` and the caller | Verification scope, exception authority, remediation authority, and delivery decisions |

GolangCI-Lint must not decide whether a finding blocks delivery. It reports a
policy violation. Repository governance decides what happens next.

## Provisional policy direction

The following set is a calibration candidate. It is not an approved
`.golangci.yml`.

### Candidate blocking checks

| Concern | Candidate linters | Provisional settings |
| --- | --- | --- |
| Core correctness | `govet`, `staticcheck`, `ineffassign`, `unused`, `exhaustive`, `durationcheck` | Use documented defaults unless the baseline proves a repository need |
| Error handling | `errcheck`, `errorlint`, `nilnesserr` | Trial `errcheck.check-type-assertions: true` and `check-blank: true` |
| SQL ownership | `rowserrcheck`, `sqlclosecheck` | Use documented defaults |
| Function shape | `cyclop`, `gocognit`, `funlen`, `dupl` | Trial 12, 15, 80 lines and 50 statements, and 120 tokens respectively |
| Interfaces | `interfacebloat`, `iface` | Trial five methods and only the `identical` analyzer |
| Imports | `depguard` | Define explicit rules from the live package graph |
| Exceptions | `nolintlint` | Require a specific linter, an explanation, and no unused directives |
| Formatting | `gofmt` | Configure as a v2 formatter check |

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

### Candidate checks to omit initially

- Omit `cyclop.package-average`. Trivial functions can lower the average, and
  package size changes the meaning of the number.
- Omit `maintidx` until it proves an actionable gap beyond CodeScene and the
  direct shape metrics.
- Omit `nilerr` when `nilnesserr` provides the combined analysis.
- Omit `nilnil` until the plan decides how to represent legitimate no-op
  mutations such as `stageIntent`.
- Omit `bodyclose` and `noctx` until production owns an HTTP client path.
- Omit the package-local `iface` analyzers that may misread exported or
  intentionally opaque interfaces.

## Candidate dependency policy

Use `depguard` to preserve the current deep-module design. Do not invent a
generic domain, repository, and HTTP layer.

The live production imports support this first rule map:

| Files | Candidate allowed project imports |
| --- | --- |
| `internal/model/**`, `internal/configuration/**` | None |
| `internal/artifact/**`, `internal/provenance/**`, `internal/result/**`, `internal/subject/**` | `reviewparty/internal/model` |
| `internal/store/**` | `reviewparty/internal/model`; retain the approved `modernc.org/sqlite` adapter dependency |
| `internal/engine/**` | `artifact`, `configuration`, `model`, `provenance`, `result`, `store`, and `subject` |
| `cmd/review-party/**` | `configuration`, `engine`, `model`, `provenance`, `store`, and `subject`; retain the approved CLI dependencies |

These rules remain provisional until test imports and negative fixtures prove
that the path matching is correct. A rule that merely reproduces today's graph
without expressing intended ownership is not ready.

## Research plan

### 1. Decide how to provision and pin the tool

Status: **Blocked on dependency approval**

1. Recheck the latest stable v2 release and Go 1.26 support.
2. Compare an exact release binary, a repository version file, and the official
   GitHub Action installation path.
3. Decide whether the workflow pins actions by immutable commit SHA or release
   tag.
4. Define a checksum or provenance check for locally downloaded binaries.
5. Keep all calibration binaries and caches under `scratch/`.
6. Confirm that the chosen method does not modify `go.mod` or `go.sum`.

Completion criterion: record one exact linter version, one local installation
method, one CI installation method, and the commands that prove each installed
binary has the expected version. Obtain explicit approval before installing the
tool.

### 2. Produce the real baseline

Status: **Blocked on research step 1**

1. Write the smallest candidate v2 configuration under `scratch/`.
2. Run `golangci-lint config verify` against that file.
3. Run the candidate configuration against all nine packages.
4. Save machine-readable output under `scratch/`.
5. Classify every finding by linter, package, production or test code, and
   likely disposition.
6. Separate actual defects from accepted contracts, nuisance findings, and
   code-shape warnings that would invite shallow extraction.
7. Measure cold and warm run time.

Completion criterion: every baseline finding has an evidence-backed
classification, and the planning ledger records counts and run times. A zero
exit code with diagnostics is a qualified result, not a clean baseline.

### 3. Calibrate shape rules against maintainable outcomes

Status: **Open**

1. Compare `cyclop` limits 10 and 12 on the current tree.
2. Inspect every `gocognit`, `funlen`, `nestif`, and `dupl` finding in context.
3. Describe the coherent responsibility split, if one exists, for each proposed
   remediation.
4. Reject remediations that create one-use pass-through helpers, widen an
   interface, or scatter one module's knowledge.
5. Materialize selected `code-quality` eval cases under `scratch/` as secondary
   calibration examples. Do not use fixture scores as production thresholds.
6. Review several recent agent-authored changes to learn whether the policy
   catches the code shapes agents actually introduce.

Completion criterion: every blocking shape rule has a chosen threshold, at
least one representative positive example, and at least one clean example that
the rule leaves alone. The recorded remediation must deepen or preserve the
owning module.

### 4. Prove the dependency rules

Status: **Open**

1. Generate the direct production and test import graph from `go list`.
2. Write explicit `depguard` rules for each package group.
3. Create disposable scratch cases for representative forbidden imports.
4. Prove that the current tree passes.
5. Prove that each forbidden import fails with the expected rule name.
6. Check whether test files need a narrow exception. Prefer test behavior
   through the same module interface over cross-package access.

Completion criterion: the rule map states intended ownership, the current tree
passes, and every protected dependency direction has a failing negative case.

### 5. Decide the error and suppression contracts

Status: **Open**

1. Classify each `errcheck` result, including ignored cleanup and process
   termination results.
2. Decide whether accepted best-effort operations return an aggregate error,
   emit a narrowly explained `//nolint:errcheck`, or use another explicit
   contract.
3. Decide whether `stageIntent` should keep `nil, nil` as a no-op or model the
   outcome explicitly before considering `nilnil`.
4. Inventory every alternate suppression form recognized by the enabled
   linters.
5. Choose one repository check that rejects alternate forms and accepts only a
   specific, explained `//nolint:<linter>` directive.

Completion criterion: every ignored result and every allowed suppression form
has one documented policy. The plan contains no global function-name or path
exclusion used only to silence findings.

### 6. Decide the local and CI interfaces

Status: **Open**

1. Decide whether this repository should add its first GitHub Actions workflow
   for linting or use another hosted gate.
2. Keep the complete CI command equivalent to `golangci-lint run` at the module
   root.
3. Keep local verification focused through package patterns.
4. Decide whether a small script is needed only for version verification and
   suppression auditing. Do not add a package-enumeration wrapper.
5. Decide whether CI runs tests and lint in parallel jobs.
6. Run a trial workflow that proves configuration verification, annotations,
   cache behavior, and failure reporting.
7. Record the cold and warm CI duration.

Completion criterion: the final plan names the exact local commands, CI events,
workflow permissions, pinned action identities, cache behavior, and maximum
accepted run time.

### 7. Write and test the agent policy

Status: **Open**

Draft a compact `AGENTS.md` addition that tells agents to treat a shape finding
as design evidence. The policy must direct an agent to improve the owning
module instead of extracting meaningless helpers. It must also define the one
allowed suppression form.

Test the draft against representative findings from research steps 2 and 3.
Record whether an agent proposes a coherent responsibility split, a justified
exception, or a metric-only rewrite. Revise the text when the observed behavior
misses the intended result.

Completion criterion: the text changes agent behavior in the calibration cases
without restating the complete linter configuration in `AGENTS.md`.

### 8. Produce the final implementation plan

Status: **Blocked on research steps 1-7**

Create the final implementation plan only after the readiness gate passes. The
final plan must:

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

## Unanswered questions

| Question | Current leaning | Evidence or decision needed |
| --- | --- | --- |
| Which exact golangci-lint version should the repository pin? | Recheck v2.13.1 at calibration time | Dependency approval, current release, and Go 1.26 compatibility proof |
| How should local agents obtain the binary? | Exact release outside `go.mod` | Reproducible installation and integrity check |
| Should this work establish the repository's first GitHub Actions workflow? | Yes, if GitHub is the accepted delivery gate | User decision and a successful trial workflow |
| Should actions use release tags or immutable commit SHAs? | Immutable SHA with a version comment | Repository supply-chain policy decision |
| Which shape thresholds improve this code instead of rewarding helper extraction? | Start with 12, 15, 80 and 50, and 120 | Real baseline plus agent-behavior calibration |
| Does `nestif` add enough signal beyond `gocognit`? | Trial without blocking | Finding-by-finding comparison |
| Which ignored results need code changes versus explained exceptions? | Decide per operation | Actual `errcheck` baseline and ownership review |
| Does `nilnil` fit the configuration mutation contract? | Probably omit | Explicit decision about no-op mutation representation |
| What test-only imports, if any, should `depguard` allow? | Prefer none beyond the production rule | Complete test import graph and negative cases |
| Does suppression auditing need a script? | Likely, because `nolintlint` covers only `//nolint` | Enabled-linter directive inventory and a focused prototype |
| Should baseline remediation land with the configuration or in earlier slices? | Decide after counting findings | Baseline size, overlap, and reviewability |
| What run-time budget keeps local agent checks useful? | Unknown | Cold and warm measurements locally and in CI |
| Who reviews and schedules linter upgrades? | Unknown | Repository maintenance decision |

## Final-plan readiness gate

Do not call this planning work complete until all of these statements are true:

- [ ] The user approved the tool dependency used for calibration.
- [ ] The selected v2 configuration passes `golangci-lint config verify`.
- [ ] The complete baseline is recorded and every finding is classified.
- [ ] Every blocking linter and threshold has repository evidence.
- [ ] The `depguard` rules pass the current tree and fail the negative cases.
- [ ] The error-handling policy accounts for every ignored result.
- [ ] The suppression policy covers the enabled linters' alternate directives.
- [ ] The local and hosted commands use the same configuration and pinned
      linter version.
- [ ] The hosted gate, its permissions, and its run-time budget are accepted.
- [ ] The `AGENTS.md` text passes the agent-behavior calibration.
- [ ] The final implementation slices have exact files, dependencies,
      verification commands, and completion criteria.
- [ ] The user accepted the resulting implementation plan.

## Evidence log

### 2026-08-28

- Confirmed live `origin/main` at `2a10ec2`.
- Confirmed that the current worktree and `origin/main` have the same Git tree,
  `cc7d824b3295906a7aee9e460cbaef5bbcfe222b`.
- Confirmed that the packaged corpus lives under
  `internal/engine/testdata/evals`.
- Confirmed that `go list ./...` finds nine real packages.
- Confirmed that `go test ./... -count=1` passes.
- Confirmed that the repository has no lint configuration or hosted workflow.
- Confirmed that `golangci-lint` is unavailable on `PATH`.
- Confirmed that the source tree contains none of the suppression forms listed
  in the current inventory.
