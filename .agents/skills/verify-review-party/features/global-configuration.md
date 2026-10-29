# Global configuration

Review Party runs the Global Configuration's Review selection in every repository that saves none of its own, and `init --global` publishes that selection without touching any repository.

## Sub-features

- `CONFIG-GLOBAL-INIT`
- `CONFIG-GLOBAL-DEFAULT-SELECTION`
- `CONFIG-GLOBAL-TARGET-MUTATION`

## Source evidence

- `executeGlobalInit` in `cmd/review-party/profile_commands.go` routes
  `init --global` to a Global-only setup or gap report and never prepares
  repository state.
- `reportInitGaps` in `cmd/review-party/init_report.go` names the Global
  Configuration as the source of a repository's effective selection.
- `Loaded.ReviewSelection` in `internal/configuration/domain_storage.go`
  takes the Repository `reviews` field when present and the Global field
  otherwise, as a whole. `ReviewSelectionToEdit` starts a Global edit from the
  Global field and a Repository edit from the selection in force.
- `InheritGlobalReviewSelection` in `internal/configuration/intent.go` removes
  the Repository field for `config reviews inherit`, and `precedenceWarnings`
  in `internal/configuration/precedence.go` warns when a Plan moves the
  selection in force between files.
- `resolveDefaultSelection` in `internal/configuration/selection.go` records a
  Global source as selection kind `global_default` with limit source
  `global_selection`.
- `validateReviewSelection` in `internal/configuration/domain_validation.go`
  rejects a Global `reviews.repository` group.

Drift: none. Changing the Global selection from the Configuration Hub is
deferred; the Hub edits the repository selection only.

## How to get to it (user POV)

- Run `review-party init --global --baseline ...` once per machine.
- Run `review-party init --repo PATH` in any repository and read which file the selection comes from.
- Run `review-party config reviews ... --target global` to edit the Global selection.
- Run `review-party config reviews inherit --repo PATH` to drop a repository's own selection.

## Driving it with the CLI

Preconditions: Start from a fresh launched baseline. The saved Reviewer is configuration only; this recipe never proves Reviewer execution.

- **Check readiness.** Run `.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"` and require `doctor: ready`.
- **Read the gap report.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/02-init-global-gaps.txt -- review-party init --global`. It exits `0` and every suggested command carries `--global`.
- **Publish the Global selection.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/03-init-global-baseline.txt -- review-party init --global --baseline --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --yes`. The Plan reports four Global Profiles, the `baseline` Party, and a `global reviews` change from `<absent>` to a selection whose `global` group holds `{"party":"baseline"}` and whose `repository` group is empty.
- **Rerun for idempotence.** Capture the same command at `global-configuration/04-init-global-rerun.txt`. It exits `0` and reports that the selection already includes the baseline.
- **Read the effective selection.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/05-show-after.txt -- review-party config show --repo "$run_dir/runtime/repository" --format json`. `reviews.kind` is `global_default`, `reviews.source` is `$run_dir/runtime/config/review-party/config.json`, `reviews.limit_source` is `global_selection`, and the expanded Profiles are the four Global baseline Profiles.
- **Report from the repository.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/06-init-repo-reports-global.txt -- review-party init --repo "$run_dir/runtime/repository"`. The report prints `Review selection: Global Configuration` followed by the Global path, the four effective Profiles, and `Repository concurrency: 4`. `$run_dir/runtime/repository/.reviewparty` does not exist afterwards.
- **Validate without a repository file.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/07-validate.txt -- review-party config validate --repo "$run_dir/runtime/repository" --format json`. The result is valid and lists the repository file with `present: false`.
- **Edit the Global selection by target.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/08-concurrency-global.txt -- review-party config reviews set-concurrency 2 --target global --format json --yes`. The single change has scope `global` and the Global path, and `review-party config file show --scope global --format json` captured at `global-configuration/12-file-show-global.txt` shows `concurrency_limit` `2`.
- **Run on the Global default.** Add a working change to `$run_dir/runtime/repository`, then run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" global-configuration/13-run-global-default.txt -- review-party run --repo "$run_dir/runtime/repository" --format json` with exit-on-error disabled. The bundle JSON's `bundle.selection.kind` is `global_default`, its `source` is the Global path, and it holds one review per baseline Profile. With no Reviewer in the owned HOME, each review ends incomplete; record Reviewer execution as unproved. `review-party stats --format json` captured at `global-configuration/16-stats.txt` counts the runs under `by_selection` name `global_default`.

Negative controls: Capture `review-party config reviews add --target global --scope repository --profile bugs --format json --yes` at `global-configuration/09-reject-repository-group.txt` with exit-on-error disabled. It exits `1`, `valid` is `false`, and `reason` contains `reviews.repository selects Repository definitions, which Global Configuration cannot name`. Capture `review-party init --global --repo "$run_dir/runtime/repository"` at `global-configuration/10-global-excludes-repo.txt`; it exits `2` and names `[global repo]` as mutually exclusive. Run doctor afterwards.

## Gotchas

- `--scope` on `config reviews` names the selection group inside a file. `--target` names the file. Mixing them up produces the cannot-name rejection, not a flag error.
- A Repository `reviews` field replaces the Global one as a whole. A repository that saves any selection no longer sees the Global one.
- A run against the Global default without a Reviewer on the owned PATH exits nonzero with incomplete reviews. That is the expected Reviewer-unavailable outcome, not a selection failure.
