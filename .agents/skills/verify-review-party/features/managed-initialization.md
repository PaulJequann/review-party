# Managed initialization and history

Review Party initializes managed state for a Git repository, permits the same initialization again, and queries existing history without launching a Reviewer.

## Sub-features

- `STATE-INIT-IDEMPOTENT`
- `STATE-HISTORY-EMPTY`

## Source evidence

- `newInitCommand` in `cmd/review-party/standard_commands.go` exposes `init`
  with explicit repository, state, and configuration paths.
- `newHistoryCommand` in `cmd/review-party/standard_commands.go` exposes
  `history` with the explicit repository and configuration inputs used by
  this recipe.

Drift: none. The live recipe below verifies the missing-repository rejection,
idempotent initialization, and empty history view.

## How to get to it (user POV)

- Run `review-party init --repo PATH --state-dir PATH --config PATH`.
- Run `review-party history --repo PATH --config PATH --format json`.

## Driving it with the CLI

Preconditions: Start from a fresh launched baseline. The launch already performed the first initialization.

- **Check readiness.** Run `.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"` and require `doctor: ready`.
- **Repeat initialization.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" managed-initialization/reinit.txt -- review-party init --repo "$run_dir/runtime/repository" --state-dir "$run_dir/runtime/state/review-party" --config "$run_dir/runtime/config/review-party/config.json"`. The command exits `0`. Without a terminal it writes no configuration and prints only the remaining report, one line per missing piece with the command that adds it, or `Repository is ready: review-party run --repo PATH`.
- **Read history through a second view.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" managed-initialization/history.json.txt -- review-party history --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json`. The JSON contains an empty `entries` array, `limit` is `20`, and `has_more` is `false`.

Negative control: Before the positive recipe, run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" managed-initialization/negative-missing-repository.txt -- review-party init --repo "$run_dir/runtime/missing-repository" --state-dir "$run_dir/runtime/state/review-party" --config "$run_dir/runtime/config/review-party/config.json"`. The repository path check must exit nonzero and report that it cannot resolve the missing target. The command must not create that path or remove or replace the existing managed state. Restore the baseline by rerunning doctor.

## Gotchas

- History does not initialize missing state. A history success is meaningful only after doctor verifies the owned ledger.
- Always pass the explicit configuration path with the explicit state directory. Mixing default XDG state with the run-owned configuration invalidates isolation.
