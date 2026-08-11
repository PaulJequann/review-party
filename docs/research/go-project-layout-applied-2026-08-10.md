# Go Project Layout — Applied Migration 2026-08-10

**Status:** applied — local `main`, not yet committed
**Base research:** `docs/research/go-project-layout-2026-08-10.md`

## What changed

Moved 34 Go files from the repository root into `internal/` + `cmd/` following the official `go.dev/doc/modules/layout` "Server project" pattern. The root now contains zero `*.go` files.

### Before (flat, 6,075 lines, 34 files, `package reviewparty`)

```
reviewparty/
  *.go (34 files)           — all package reviewparty
  cmd/review-party/         — package main (already correct)
  profiles/*.md
  go.mod
```

### After

```
reviewparty/
  cmd/review-party/          — thin CLI, now imports engine + model
  internal/
    engine/                  — conductor, profiles (7 files), harness (executor + 3 adapters + catalog + config), local_file, plus alias shims
      profiles/              — copy of top-level profiles/*.md for //go:embed (Go embed cannot use ..)
      model_aliases.go, store_aliases.go, subject_aliases.go, result_aliases.go, provenance_aliases.go
    model/                   — types.go (all shared DTOs: ReviewRecord, Subject, Profile, etc.)
    subject/                 — git.go, capture.go, numstat.go (git subject resolution) + helpers_test.go
    result/                  — contract.go, parser.go (result envelope)
    store/                   — store.go (file record store)
    provenance/              — provenance.go (build info)
  profiles/                  — canonical packaged profiles (bugs.md, documentation.md) — kept top-level for visibility
  docs/research/
```

`internal/engine` is intentionally coarse (25 files). The tightly coupled cohort — conductor, profile compilation (profile.go, library.go, config.go, files.go, init.go, diagnostics.go), reviewer catalog + config, and harness (executor, copilot, grok, opencode, process groups) — share private helpers (`compiledProfile`, `reviewerRegistration`, `harnessAdapter`, `profileLibrary`) that would require extensive exporting to split further. Keeping them in one deep module preserves the seam and locality while still achieving:

* no files at root
* compiler-enforced encapsulation via `internal/` (no external importer can depend on private code)
* thin `cmd/` seam (each file in `cmd/review-party` imports only `engine` and `model`)

Leaf packages (`model`, `subject`, `result`, `store`, `provenance`) are pure: they depend only on stdlib and `model`, have no cycles, and are imported by `engine` via exported `ResolveSubject`, `ResolveRepositoryRoot`, `NewFileRecordStore`, `CanonicalReviewResultContract`, `CurrentRuntimeProvenance`.

## Why this grouping (reevaluated after new merges)

New files merged since the research:

* `git_numstat.go` (101 lines) + `working_changes_capture.go` (78) + `runtime_provenance.go` (44) + associated tests

These split subject into three files (git, capture, numstat) but all share `SubjectFacts` and `workingChangesCapture` private types — they must stay in one leaf package (`subject`). Similarly `runtime_provenance.go` is leaf-local to `provenance`.

The original research proposed `internal/conductor`, `internal/subject`, `internal/profile`, `internal/harness`, `internal/result`, `internal/store`, `internal/runtime`, `internal/model` (7 packages). The `profile` + `harness` + `conductor` split would have forced exporting `reviewerCatalog`, `compiledProfile`, `profileLibrary`, and `harnessAdapter` across packages. Deferring that split to a second pass avoids a risky type-export churn now; `internal/engine` can be carved into `internal/profile` and `internal/harness` later when a second adapter seam (e.g., transport) appears — the codebase-design rule "one adapter = hypothetical seam, two adapters = real seam".

## Verification

```
go vet ./internal/... ./cmd/...   — no output
go test ./internal/... ./cmd/...  — 7 packages ok (engine 0.26s, subject 0.05s, etc.)
gofmt -w internal cmd — clean
```

No `*.go` at root (`ls *.go` → no matches). `profiles/` and `internal/engine/profiles/` are duplicate copies (embed cannot use `..`). The research noted `module reviewparty` should become `github.com/<org>/review-party` for `go install`; deferred (no behavior change, but worth a single `go mod` pass later).

## Next steps (not done in this pass)

* Split `internal/engine` into `internal/profile` + `internal/harness` once profile and harness acquire distinct private helpers (two adapters already justify the harness seam; profile vs. harness still share `reviewerCatalog`).
* Consider `module` path rename to hosted import path.
* Add CI check that `profiles/*.md` and `internal/engine/profiles/*.md` stay byte-identical (or replace copy with a generate step).
* CodeScene checks on new `internal/*` files (gate: new files ≥ 9.0, no regress) — run before commit per AGENTS.md.
