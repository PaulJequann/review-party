# Agent Instructions

## Repository Purpose

Review Party is a purpose-built code-review CLI. It will resolve review
subjects, compile named review profiles into bounded execution plans, route
those plans through agent adapters, and produce validated review packages.

Keep review intent, agent choice, and transport choice separate. A shared
result contract does not require every agent to use the same wire protocol.

## Agent Rules

- Keep local verification focused on the files and packages changed. Run the
  smallest relevant test, lint, format, and build checks; do not use a full
  repository verification command as a routine completion step.
- Stop development servers, watchers, and other long-running verification
  processes when focused verification is complete.
- Do not add or install dependencies without explicit user approval.
- Repository-pinned tools from `scripts/` may be bootstrapped.
- Use `./scripts/lint.sh` as the lint entrypoint; do not invoke a global
  `golangci-lint`.
- Do not install or bump tools globally.
- Do not edit lint policy files unless the task explicitly authorizes a policy
  change: `.golangci.yml`, `.golangci-lint-version`, `scripts/lint.sh`,
  `scripts/verify-lint-suppressions.sh`, the lint workflow, and `depguard`
  rules.
- Treat shape findings (`cyclop`, `gocognit`, `funlen`, `dupl`, and
  `interfacebloat`) as design evidence, not extraction orders. Improve the
  owning module or add a specific, explained `//nolint:<linter>` exception when
  the existing design is preferable. Keep `inspectRootedPath` and
  `applyCodexEvent` unshredded; use the shared JSONL scanner in
  `internal/engine/jsonl.go` as the positive extraction example. Do not extract
  one-use pass-through helpers, widen an interface, or scatter one module's
  knowledge.
- A lint failure is not permission to weaken `.golangci.yml`, raise thresholds,
  add path exclusions, skip the suppression auditor, or edit `depguard`.
- Treat incomplete external-agent runs as incomplete, never clean.
- Do not silently substitute an agent, model, transport, profile, or weaker
  capability contract. Report the unavailable choice and require the caller's
  authorization for a declared fallback.
- Keep project governance outside the review engine. The CLI may report facts
  and validated results, but repository instructions and the caller decide
  whether a finding blocks delivery or whether remediation is authorized.
- Treat the product as pre-release with no existing-user compatibility burden.
  Prefer direct replacement over migrations, backfills, legacy readers, or
  compatibility branches. Surface obsolete code and files for removal; when a
  tracked path should be deleted, request the path-specific approval required
  below rather than retaining dead code.

## Dogfooding

When running Review Party against the current checkout as a bounded local
dogfood review, load and follow the `dogfood-review-party` skill.

## Verification

To verify a CLI or Configuration Hub change on the real binary, load and
follow the `verify-review-party` skill.

## No Deletions (Absolute)

You may **not** delete any file or directory without explicit user approval for
the specific path or paths.

The following deletions are allowed without approval:

- Files you created in the current session that are untracked and unreferenced.
- Files in `/tmp` or `./scratch`.
- Gitignored build artifacts such as `dist/`, `bin/`, coverage output, and tool
  caches.

Deletion approval is path-specific. Approval for one path does not authorize
deleting adjacent, generated, obsolete-looking, or replacement files.

## Scratch Work

- Use `./scratch/` for repository-level experiments, captured command output,
  benchmark fixtures, and temporary artifacts.
- `./scratch/` is gitignored and may be wiped without approval.
- Durable research belongs in `docs/research/`, not `./scratch/`.
- Do not use tracked source directories for temporary experiments.

## Branching Policy

- Work on the local `main` branch by default.
- Recommended branch names follow `<type>/<short-slug>`, where `<type>` is
  `feat`, `bugfix`, `chore`, `refactor`, `docs`, or `test`.
- An explicit user preference overrides these defaults.

## Code Health (Mandatory)

Run CodeScene on every touched or new source file after all edits and before
committing. Documentation, configuration, fixtures, and generated artifacts are
not source files unless they contain executable program logic.

When CodeScene MCP tools are available, use this bounded flow:

- Score an existing source file once before its first edit and once after all
  edits are complete. New source files need only the final score.
- Do not rescore unchanged content. Rerun a score only after code changes made
  in response to a finding.
- Run `code_health_review` only when the final score regresses, is below 9.0, a
  required improvement is unclear, or CodeScene cannot score the source.
- Run `pre_commit_code_health_safeguard` once before committing the final local
  change set. Rerun it only after relevant code changes.
- Run `analyze_change_set` only for a requested branch or pull-request review.

Apply these gates:

- New source files that CodeScene can analyze must score at least 9.0.
- Existing files must not regress.
- Resolve introduced findings and existing findings overlapping functions being
  changed.
- When an existing file is below 10.0, make at least one safe, relevant
  improvement in the same responsibility. If that would materially expand the
  task, report the exact finding and reason.
- Treat a missing or unsupported score as inconclusive, not a pass.
- Do not suppress findings, commit a regression, or lower thresholds to make a
  gate pass.

If CodeScene is unavailable, report that plainly and use focused tests plus the
language's formatter, linter, and compiler as the available evidence. Do not
claim the CodeScene gate passed.

## Research and References

- Use primary sources: official documentation, specifications, source code, and
  first-party APIs.
- Clearly distinguish sourced evidence from architectural inference.
- Save durable research notes under `docs/research/` with direct source links.
- If external repositories are later vendored under `.repos/`, treat them as
  read-only reference material unless the user explicitly authorizes edits.

## Go Conventions

- Use the Go standard library by default. Adding a third-party module requires
  explicit approval.
- Keep agent-specific launch quirks behind adapters rather than leaking them
  into review profiles or domain types.
- Prefer argv-based subprocess execution; do not construct shell command strings
  from profile or user input.
- Make cancellation, process cleanup, timeouts, and incomplete-result semantics
  explicit at adapter boundaries.
- Run `gofmt` on changed Go files and targeted `go test` commands for affected
  packages. Do not run unrelated integration or live-agent suites routinely.

## Linear issue tracking

Issues are tracked in Linear under team `DEV`.

- Repository label: `Repository/Review Party`
- Use the global `capture-linear-issue` skill to create issues.
- Use the global `investigate-linear-issue` skill to investigate issues.
- Do not create repository-local backlogs, task databases, or issue caches.
- Do not implement issues marked `Readiness/Needs investigation` unless the
  user explicitly overrides the workflow.
