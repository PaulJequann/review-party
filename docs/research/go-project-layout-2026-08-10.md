# Go Project Layout: Standards and Recommendation for Review Party

**Date:** 2026-08-10
**Status:** research — not an accepted architecture decision
**Author:** OpenCode (Muse Spark)
**Question:** How should a modern Go CLI repository like Review Party be structured so that 30+ files at the repository root do not become an unmaintainable flat namespace, and what do official Go guidance and popular modern Go projects actually recommend?

## TL;DR Recommendation

For Review Party — a self-contained CLI with **no exported library** — follow the official Go guidance's **"Server project"** pattern, not the community `golang-standards/project-layout` template:

```
review-party/
  cmd/review-party/          # already exists — thin main, flag parsing
  internal/
    conductor/               # conductor.go, store.go, types.go (lifecycle)
    subject/                 # subject_git.go, subject resolution
    profile/                 # profile*.go (7 files) + profiles/ embed
    config/                  # configuration.go, reviewer_catalog.go
    reviewer/                # copilot.go, grok.go, opencode.go, local_file.go
    executor/                # executor.go, process_group_*.go
    result/                  # result_contract.go, result_parser.go
  profiles/                  # keep as data dir OR move under internal/profile/testdata
  docs/
  scripts/                   # if Makefile grows
  .codescene/
```

*   **Do not** adopt `/pkg`, `/api`, `/web`, `/build`, `/deployments` from `golang-standards/project-layout`. Those are for mixed repos with exportable libraries or web services.
*   **Do** move every `package reviewparty` file off the root into `internal/<domain>/`. This is the single highest-leverage change — it restores module depth and locality before the repo doubles in size.
*   **Do not** create `src/` — the Go team explicitly discourages it.

Details and sourcing below.

---

## 1. What the Official Go Team Actually Says

**Primary source:** [Organizing a Go module](https://go.dev/doc/modules/layout) — go.dev, authored by the Go team. This is the canonical layout guide (not the community `golang-standards` repo).

Key principles, quoted and cited:

1.  **Start flat, split when needed.** A basic package or command lives entirely at the root with a single `go.mod` and `package modname` or `package main` files. There is no prescribed `internal/` or `cmd/` until complexity demands it. [go.dev — Basic package](https://go.dev/doc/modules/layout#basic-package), [Basic command](https://go.dev/doc/modules/layout#basic-command)

2.  **`internal/` is the endorsed seam for hiding code.** As soon as a package or command needs supporting code that external importers should not depend on, put it in `internal/`. The Go compiler enforces that no module outside the parent tree can import from `internal/`. This lets you refactor the API freely. Use `internal/` as much as possible. [go.dev — Package or command with supporting packages](https://go.dev/doc/modules/layout#package-or-command-with-supporting-packages), [Multiple packages](https://go.dev/doc/modules/layout#multiple-packages)

3.  **`cmd/` for multiple binaries.** Each program gets its own directory with a `main.go` declaring `package main`. A top-level `internal/` can hold shared code used by all commands. Putting commands under `cmd/prog1`, `cmd/prog2` is the common convention, especially in repos that have both libraries and binaries. [go.dev — Multiple commands](https://go.dev/doc/modules/layout#multiple-commands), [Packages and commands in the same repository](https://go.dev/doc/modules/layout#packages-and-commands-in-the-same-repository)

4.  **Server / CLI / tool projects keep Go code in `internal/` + `cmd/`.** For self-contained servers (and by extension CLIs, tools, agents) that do not export packages, the guidance is: keep logic in `internal/auth`, `internal/metrics`, `internal/model`, keep binaries together under `cmd/api-server`, `cmd/metrics-analyzer`, and leave other top-level directories for non-Go concerns. Split exportable packages into separate modules if they arise later. [go.dev — Server project](https://go.dev/doc/modules/layout#server-project)

5.  **Hierarchical packages are fine.** A module may have many importable packages, each in its own directory (`auth/token/` etc.). [go.dev — Multiple packages](https://go.dev/doc/modules/layout#multiple-packages)

6.  **`go get`-able import path.** The module path should be its hosting path. This repo uses `module reviewparty`; the guidance says it should declare the hosting import path (e.g. `github.com/<org>/review-party`). This does not affect internal layout but matters for any future `go install` usage. [go.dev — Organizing Go code (blog, 2012)](https://go.dev/blog/organizing-go-code#choose-a-good-import-path)

**What the Go team does NOT prescribe:** Structure under `/pkg`, `/api`, `/web`, `/configs`, `/scripts`, `/build`, `/deployments`, `/test` — those are not in `go.dev/doc/modules/layout` at all.

---

## 2. What `golang-standards/project-layout` Prescribes — and Why to Treat It With Caution

**Primary source:** [golang-standards/project-layout](https://github.com/golang-standards/project-layout) (56k stars, but explicitly **not official**).

Relevant claims:

*   `This is NOT an official standard defined by the core Go dev team... a set of common historical and emerging project layout patterns.` [project-layout README — Overview](https://github.com/golang-standards/project-layout#overview)
*   `If you are trying to learn Go or if you are building a PoC or a simple project for yourself this project layout is an overkill. Start with something really simple... a single main.go file and go.mod is more than enough.` [project-layout README — Overview](https://github.com/golang-standards/project-layout#overview)
*   Defines optional directories: `/cmd` (one dir per executable), `/internal` (private code, compiler-enforced), `/pkg` (exportable libraries), `/vendor`, plus service/web/common/other dirs: `/api`, `/web`, `/configs`, `/init`, `/scripts`, `/build`, `/deployments`, `/test`, `/docs`, `/tools`, `/examples`, `/third_party`, `/githooks`, `/assets`, `/website`. [project-layout README — Go Directories / Service / Web / Common / Other](https://github.com/golang-standards/project-layout#go-directories)
*   Guidance on `/cmd`: directory name should match executable name, keep `main` small and import from `internal`/`pkg`. [project-layout README — /cmd](https://github.com/golang-standards/project-layout#cmd)
*   Guidance on `/internal`: code you do not want others importing; may have multiple `internal` dirs at any tree level; suggests optional `internal/app` and `internal/pkg` separation. [project-layout README — /internal](https://github.com/golang-standards/project-layout#internal)
*   Guidance on `/pkg`: library code safe for external use. Notes that `internal` is the stronger mechanism (compiler-enforced) and `/pkg` is a communication convention. Some Go community members recommend against `/pkg` entirely. `It's ok not to use it if your app project is really small...` [project-layout README — /pkg](https://github.com/golang-standards/project-layout#pkg)
*   Explicitly says **Do not use `/src`** — a Java carryover that collides with `GOPATH` workspace layout. [project-layout README — /src](https://github.com/golang-standards/project-layout#src)

**Assessment for Review Party:** The template is intentionally generic and maximal — `Clone the repository, keep what you need and delete everything else! Just because it's there it doesn't mean you have to use it all.` For a focused CLI with zero exportable library intent, most of the template is noise. Following it wholesale produces a repo that looks structured but scatters a 5k-line codebase across ceremonial directories (`configs/`, `deployments/`, `web/`, `api/`) that will stay empty.

---

## 3. How Popular Modern Go Projects Actually Organize Themselves

### 3.1 `kunchenguid/no-mistakes` — project the user cited (7.5k stars, Go CLI gate)

**Primary source:** GitHub directory listing at [kunchenguid/no-mistakes](https://github.com/kunchenguid/no-mistakes) and [kunchenguid/no-mistakes/tree/main/internal](https://github.com/kunchenguid/no-mistakes/tree/main/internal).

Observed layout (2026-08-10 snapshot):

```
no-mistakes/
  cmd/                          # entrypoints
  internal/                     # 35 sub-packages, every domain its own package
    agent/  bitbucket/  branchsync/  buildinfo/  cimonitor/  cli/
    config/  conventional/  daemon/  db/  e2e/  e2edaemon/  evidence/
    gate/  gatecontext/  gateguidance/  git/  intent/  ipc/  lifecycle/
    logstore/  paths/  pipeline/  procreap/  safeurl/  scm/  shellenv/
    skill/  telemetry/  testguidance/  tui/  types/  update/  winproc/  wizard/
  docs/
  scripts/
  skills/no-mistakes/
  .no-mistakes/
  go.mod    Makefile    README.md
  *_test.go at root — only 7 workflow/guard contract tests
```

What it demonstrates:

*   Almost all Go code lives in `internal/<domain>/`, one package per seam. The root holds zero domain files — only `go.mod`, repo metadata, and ~7 workflow contract tests that validate generated CI YAML.
*   `cmd/` is present, `internal/` is the entire engine, `pkg/` is absent (nothing is exported).
*   Domains are vertical slices, not layers: `gate/`, `pipeline/`, `cli/`, `tui/`, `scm/`, `git/`, `skill/`, `ipc/` — each package owns its behavior behind a small interface.
*   Shares the "Server project" shape from go.dev, scaled to ~35 packages because the product has many distinct responsibilities (daemon, worktree isolation, TUI, skill distribution).

Relevance: `no-mistakes` is the closest modern analogue to Review Party — a Go CLI that orchestrates external agents behind a gate. Its choice to put *everything* in `internal/` is deliberate for encapsulation and would be the model to copy.

### 3.2 `cli/cli` — GitHub's official CLI (`gh`, 45.8k stars)

**Primary source:** GitHub listing at [cli/cli](https://github.com/cli/cli).

Observed top-level:

```
cli/
  cmd/gh/               # entrypoints (multiple: cmd/gen-docs etc.)
  api/                  # GitHub API client — importable but niche
  git/  context/  utils/ # legacy top-level packages (predate internal/)
  internal/             # newer: internal/config, internal/build, etc.
  pkg/                  # pkg/cmd/* (one pkg per gh command), pkg/iostreams, etc.
  script/  build/  docs/  test/  acceptance/
```

What it demonstrates:

*   Hybrid legacy layout: `api/`, `git/`, `context/`, `utils/` at the root predate `internal/` enforcement; newer code goes to `internal/` and `pkg/cmd/*`.
*   Heavy use of `pkg/cmd/<command>/` — each gh subcommand (pr, issue, repo...) is its own package exporting a `NewCmd...` constructor. This makes sense when commands are independently testable and the repo is a large command suite.
*   `cmd/gh/main.go` is thin and delegates to `pkg/cmd/root`.

Relevance: Lower than `no-mistakes` for Review Party. `cli/cli` is an outlier in size (1k+ files, dozens of subcommands) and maintains `pkg/` because `pkg/iostreams`, `pkg/search` etc. are reused across many commands. Review Party currently has one entry `cmd/review-party` and no need for per-command packages.

### 3.3 `golang/go` and the Standard Library (reference)

The Go toolchain itself puts tools under `src/cmd/go/` (12k lines, 34 files, `package main` plus supporting files) and private helpers under `src/internal/` — consistent with the internal/cmd split. This reinforces the `go.dev` guidance: `package main` may legitimately be large; otherwise use `internal/`.

### 3.4 Common Critique of `/pkg`

Multiple primary sources echoed in `project-layout` and the Go blog: `/pkg` is **not compiler-enforced**, unlike `internal/`. [Travis Jeffery — I'll take pkg over internal](https://travisjeffery.com/ill-take-pkg-over-internal/) (linked from project-layout) and Go wiki discussions note that `/pkg` is a social contract; `internal/` is a guarantee. For Review Party, which today has no external consumers, `/pkg` would mis-signal a supported public API. If a public library ever emerges (e.g., a result-contract parser), it should be split to its own module rather than published via `pkg/`.

---

## 4. Current Review Party Structure — Diagnosis

Snapshot 2026-08-10 — repository root:

```
review-party/
  *.go (32 files, 5,311 lines total) all `package reviewparty`
  cmd/review-party/ (8 files, package main — correct)
  profiles/*.md      (embedded data via profile_library.go embed)
  go.mod (module reviewparty, go 1.26, un-hosted path)
  docs/, .gitignore, AGENTS.md, README.md
```

Flat-file inventory at root (32 files, grouped by responsibility):

| Group | Files | Lines | Responsibility |
|-------|-------|-------|----------------|
| Conductor | `conductor.go` (329), `store.go` (88), `types.go` (190), `subject_git.go` (153) | ~760 | Lifecycle, Attempt binding, subject resolution |
| Profiles | `profile.go` (308), `profile_library.go` (176), `profile_config.go` (179), `profile_files.go` (204), `profile_init.go` (257), `profile_diagnostics.go` (61), `profile_test.go` + inventory/library/init tests | ~1,200 | Profile compilation, inventory, init |
| Configuration | `configuration.go` (290), `reviewer_catalog.go` (120) | ~410 | User configuration, reviewer capability catalog |
| Adapters | `copilot.go` (149), `grok.go` (110), `opencode.go` (121), `local_file.go` (117) | ~500 | Agent adapters (each behind same executor seam) |
| Execution | `executor.go` (308), `process_group_unix.go` (24), `process_group_other.go` (17) | ~350 | Harness execution, timeouts, process groups |
| Result | `result_contract.go` (40), `result_parser.go` (169) | ~210 | Result envelope validation + parsing |
| Tests alongside | `*_test.go` (8 files) | ~1,100 | Co-located, correct |
| `cmd/review-party/` | 8 files (main, config, profiles, profile_commands) | — | CLI wiring — already well-placed |

**Problems with "files at the root":**

*   **No locality.** A change to profile compilation touches 5 files that sit next to executor, adapter, and result code. `grep` and code review both lose the signal that `profile_*` is a module.
*   **Shallow namespace.** Every identifier shares one package (`reviewparty`). You cannot enforce seams; any file can reach any unexported symbol. The codebase-design principle of a **seam** — the place you can swap behavior without editing the call site — is impossible when there is a single package boundary.
*   **JS/TS mental-model mismatch is real.** In TS/JS you expect folders to mirror domain boundaries and imports to declare dependencies. Go's equivalent is *packages are directories*; a Go package is the smallest deployable unit with an interface. Having everything in `package reviewparty` is like having a TS project with 30 files all re-exported from a single `index.ts` barrel and no folder-level `import` graph — the tooling cannot help you.
*   **Growth ceiling.** At 5k lines the current shape is still navigable (the go.dev docs call this the "basic package/command" stage). At 10–15k lines — likely if Party, multi-pass, and synthesis features land per `docs/product-model.md` — the flat layout becomes a merge-conflict and discovery bottleneck. `no-mistakes` crossed that threshold and moved to 35 internal packages; `cli/cli` did too.

**What is already well-placed:** `cmd/review-party/` follows the official `cmd/` convention; `_test.go` co-located with source follows idiomatic Go; `profiles/*.md` as data is fine (whether it stays top-level or moves under `internal/profile/` is a separate embedding choice).

---

## 5. Recommended Structure for Review Party — Incremental, Go-Idiomatic, Aligned to Product Model

### 5.1 Principles Applied

*   **Follow `go.dev/doc/modules/layout`, not `golang-standards` maximalism.** Review Party is a Server/CLI project: `internal/` + `cmd/`.
*   **Packages are modules; directories are seams.** Each internal package exposes a small interface (few exported types/funcs) and hides a deep implementation. Apply the deletion test: if you delete `internal/reviewer`, adapter complexity should not scatter into `conductor` — it should vanish, proving the seam carries leverage.
*   **Domains over layers.** Do not organize by `handlers/`, `services/`, `models/`. Group by the responsibility map in `docs/product-model.md`: Subject, Profile, Reviewer, Execution, Result, Conductor.
*   **Two adapters make a seam real.** The `copilot`/`grok`/`opencode`/`local_file` adapters already vary across the same executor seam — that warrants a `reviewer` package. Conversely, do not introduce an abstract `transport` package until a second transport adapter actually exists.
*   **Keep the interface the test surface.** Tests stay alongside implementation inside each `internal/<domain>/` package and import only the package's exported surface.

### 5.2 Proposed Layout (incremental — all current files have a home)

```
review-party/
  go.mod                              # consider renaming module to github.com/<org>/review-party
  README.md  AGENTS.md  CONTEXT.md
  cmd/
    review-party/
      main.go
      config.go
      profiles.go
      profile_commands.go
  internal/                            # ← all domain code moves here
    conductor/
      conductor.go          # Config, Conductor, lifecycle
      conductor_test.go
      store.go              # persistence of Review Record / Bundle
      types.go              # ReviewID, Lifecycle, SubjectReference
    subject/
      git.go                # was subject_git.go
      localfile.go          # local_file.go — file-based subject reader
      subject_test.go
    profile/
      profile.go
      library.go            # profile_library.go
      config.go             # profile_config.go
      files.go              # profile_files.go
      init.go               # profile_init.go
      diagnostics.go
      profiles/             # embed FS — moved from top-level profiles/ OR keep top-level and embed via //go:embed ../../profiles/*.md
      *_test.go
    config/
      user.go               # configuration.go (userConfiguration)
      catalog.go            # reviewer_catalog.go (capability catalog)
      *_test.go
    reviewer/               # one package, multiple adapters behind one interface
      reviewer.go           # shared Reviewer interface + registry
      copilot.go
      grok.go
      opencode.go
      local_file.go         # (or move to subject/ — see note below)
      adapters_test.go
    executor/
      executor.go           # harness execution, attempt bounds
      process_group_unix.go
      process_group_other.go
      executor_test.go
    result/
      contract.go           # result_contract.go
      parser.go             # result_parser.go
      parser_test.go
  profiles/                            # alternatively: internal/profile/profiles/
    bugs.md  documentation.md
  docs/
    research/  design/  product-model.md
  scripts/                             # only if Makefile/scripts grow
```

**Naming notes:**

*   Files inside a package lose the package prefix: `internal/profile/library.go` not `profile_library.go`, `internal/result/contract.go` not `result_contract.go`. Go convention omits stutter (`profile.Profile` is redundant — prefer `profile.Library`, `result.Contract`).
*   `types.go` should be distributed, not centralized: lifecycle types live in `conductor/`, subject types in `subject/`, profile types in `profile/`. A single `internal/types/` package creates shallow, high-fan-in coupling (every package imports `types`).
*   Whether `local_file.go` lives in `subject/` or `reviewer/` depends on whether it is a *subject reader* or an *adapter harness*; code inspection decides. Either is internally consistent.

### 5.3 What NOT to Create

| Directory | Verdict | Reason |
|-----------|---------|--------|
| `pkg/` | Do not create | No exportable library; `internal/` is compiler-enforced, `pkg/` is social. If a parser or contract ever needs to be public, split to its own module per go.dev server guidance. |
| `api/` | Do not create | No OpenAPI / protocol specs. |
| `web/` | Do not create | No web assets. |
| `configs/` | Do not create | User config is a single JSON file managed by Go code, not checked-in templates. |
| `build/`, `deployments/` | Do not create | No containers/AMI/K8s at this maturity. |
| `src/` | Must not create | Explicitly discouraged by both go.dev and project-layout. |
| `test/` top-level | Unnecessary | Tests live alongside packages (`*_test.go`). Use only if large fixture data is shared across packages. |

### 5.4 Module Path

Current `go.mod` says `module reviewparty`. Go guidance and `go install` expectations favor `module github.com/<org>/review-party`. This is worth correcting in the same change window as the `internal/` move so that any future `go install` or package import works without a rewrite. If the repo stays private, the path can still be `github.com/...` — it need not be published.

### 5.5 Migration Path (Low-Risk, Incremental)

Review Party enforces a no-deletions rule (AGENTS.md) and no unapproved renames across the repo. Apply this sequence:

1.  Create `internal/<domain>/` directories, move one domain at a time (e.g., `result/` first — only 3 files, small blast radius), update `package` clauses and imports inside `cmd/review-party/`, run `go test ./...` and `gofmt -w .` after each domain.
2.  Keep `profiles/*.md` at top-level initially; switch the embed directive to `//go:embed ../../profiles/*.md` or duplicate the embed FS path. Moving the data dir can be a follow-up.
3.  Fix `module` path last, with a single `go mod tidy` pass.
4.  Run CodeScene `code_health_score` on each moved package before/after — existing files must not regress; new directories start at ≥ 9.0.
5.  No `pkg/` promotion, no re-export barrels, no interface-layer abstractions that just pass through.

### 5.6 How This Helps When the Codebase Grows

*   `no-mistakes` needed 35 packages because daemon, TUI, pipeline, scm, and skill each grew into independent seams. Review Party's roadmap in `docs/product-model.md` (Party, multi-pass, verification/synthesis reviews) will create similar pressure. Having `internal/profile/`, `internal/executor/`, and `internal/reviewer/` already in place means those features slot into their domain without expanding the root.
*   The `internal/` seam enforces that caller code (`cmd/review-party`) can only depend on exported surfaces — the compiler catches accidental deep coupling that today hides behind the shared `reviewparty` package scope.

---

## 6. Sources

*   Go official: [Organizing a Go module](https://go.dev/doc/modules/layout) — covers Basic package, Basic command, Package/command with supporting packages, Multiple packages, Multiple commands, Packages and commands, Server project; recommends `internal/` + `cmd/` and hierarchical packages.
*   Go blog: [Organizing Go code (2012)](https://go.dev/blog/organizing-go-code) — naming, import paths, minimizing exported interface, package granularity.
*   Community template: [golang-standards/project-layout](https://github.com/golang-standards/project-layout) — overview, /cmd, /internal, /pkg, /src, /vendor, /api, /web, /configs, /scripts, /build, /deployments, /test, /docs, /tools, /examples, /third_party, /githooks, /assets, /website. Explicitly not official; includes overkill warning.
*   Travis Jeffery: [I'll take pkg over internal](https://travisjeffery.com/ill-take-pkg-over-internal/) — linked from project-layout as the `/pkg` vs `/internal` framing.
*   `kunchenguid/no-mistakes` — GitHub repo and [/internal directory listing](https://github.com/kunchenguid/no-mistakes/tree/main/internal) (2026-08-10) — 35-package `internal/` + `cmd/` CLI gate, no `pkg/`.
*   `cli/cli` — GitHub repo listing (2026-08-10) — `cmd/gh`, `internal/`, `pkg/cmd/*`, `api/`, `git/`, `utils/`, `script/`, `build/`; hybrid legacy + modern layout; per-command packages under `pkg/cmd/`.
*   Review Party local: `go.mod`, `AGENTS.md`, `docs/product-model.md`, `docs/research/*`, root `*.go` inventory (5,311 lines, 32 files, `package reviewparty`), `cmd/review-party/*`.

---

## 7. Open Questions for the Team

*   Does Review Party intend to ever export a library (e.g., `result` parser for external consumers)? If yes, the exportable subset should be identified now so the `internal/` move does not accidentally bury something that later needs promotion to `pkg/` or a separate module.
*   Is `module reviewparty` intentional (private, un-installed) or should it become `github.com/<org>/review-party` in the same restructuring?
*   Should `profiles/*.md` move under `internal/profile/` as data collocated with its compiler, or stay top-level as user-visible content? `no-mistakes` keeps skills under `skills/` top-level but implementation under `internal/skill/` — same split is reasonable.
*   Preferred granularity for `reviewer/`: one `reviewer/` package with multiple `reviewer_*.go` files (simplest) vs `reviewer/copilot/`, `reviewer/grok/` subpackages (only if adapters acquire substantial private helpers).
