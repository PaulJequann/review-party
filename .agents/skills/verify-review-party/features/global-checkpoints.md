# Global Checkpoints

Global Configuration declares Review Checkpoints that apply to every enrolled repository, where enrollment is the presence of the Repository Configuration file. A Repository `checkpoints` field replaces them as a whole.

## Sub-features

- `CHECKPOINT-GLOBAL-DECLARE`
- `CHECKPOINT-ENROLL`
- `CHECKPOINT-GLOBAL-HOOK`

## Source evidence

- `Loaded.Checkpoints` in `internal/configuration/checkpoint.go` returns no
  Checkpoints for an unenrolled repository, the Repository field when present,
  and the Global field otherwise, with the declaring file.
- `DeclareNoCheckpoints` and `InheritGlobalCheckpoints` in
  `internal/configuration/checkpoint.go` write or remove the Repository field
  for `config checkpoint none` and `config checkpoint inherit`.
- `PlanEnrollment` in `internal/configuration/checkpoint.go` stages a
  Repository Configuration file holding only the schema version.
- `enrollRepository` in `cmd/review-party/profile_commands.go` routes
  `init --enroll` through that Plan and then reports the enrolled repository's
  gaps.
- `checkpointLines` in `cmd/review-party/init_report.go` prints the enrollment
  command when the Global Configuration declares Checkpoints and the
  repository has no Configuration file.
- `label` in `cmd/review-party/checkpoint_commands.go` adds the declaring file
  to the refusal and the human report only for a Global declaration.

- `CheckpointsView.headerState` in
  `internal/configurationhub/checkpoints_view.go` is the one description of
  each file's Checkpoints that the Hub Overview, the Checkpoints browser
  headers, and the accessible file prompt share; `operations` gates which
  edits a file offers.
- `planCheckpointEdit` in `internal/configurationhub/checkpoint_editor.go` is
  the one planner the Hub TUI, the accessible Hub, and the first-use journey
  call for set, remove, inherit, opt out, and enroll.
- `undeclaredIntegrations` in `cmd/review-party/checkpoint_uninstall.go` feeds
  both the `config checkpoint` uninstall offer and the Hub's
  `RunOptions.UndeclaredIntegrations` notice.

Drift: none. The Hub declares and removes Checkpoints in both files, enrolls,
inherits, and opts out; it never installs hooks.

## How to get to it (user POV)

- Run `review-party config checkpoint set pre-push --target global --integration git` once per machine.
- Run `review-party init --repo PATH` in a repository and read the enrollment line, then `review-party init --enroll --yes --repo PATH`.
- Run `review-party checkpoint install git --repo PATH` and push.
- Run `review-party config checkpoint none --repo PATH` to turn the Global Checkpoints off for one repository, and `review-party config checkpoint inherit --repo PATH` to turn them back on.
- Or run `review-party config --repo PATH` (add `--accessible` for prompts), choose Checkpoints, and use `e`/Enter to set, `r` to remove, and on the Repository header `i` to enroll or inherit and `o` to opt out.

## Driving it with the Hub

At 120x30, open `review-party config --repo PATH` in tmux, move down to Checkpoints and press Enter (accessible mode: choose `5`). Rows are the Repository header, its pre-push and pre-commit, the Global header, and its pre-push and pre-commit. A publish returns to the menu; press Enter to reopen the area.

- **Global set with the repository unenrolled.** On the Global header press Enter, accept pre-push, and publish. Only the Global `config.json` changes; `.reviewparty` does not appear. The Global header reads `not in force here: this repository is not enrolled · pre-push`.
- **Enroll.** On the Repository header press `i`. The Plan preview carries the warning `Creating Repository Configuration enrolls this repository, so the Global pre-push Checkpoint now applies here.` After publish the Repository file is exactly `{"schema_version": 1}` pretty-printed.
- **Opt out, then inherit.** `o` on the Repository header writes `"checkpoints": {}`; `i` then removes the field.
- **Repository set prefilled from Global.** `e` on the Repository pre-push row opens the form with the Global rules.
- **Refusal.** A key the footer does not show, such as `o` on the Global header, prints `Opt out of Global Checkpoints does not apply to this row.` and opens nothing.

In accessible mode, numeric prompts do not accept an empty line as the prefilled value; type the number.

## Driving it with the CLI

Preconditions: Start from a fresh launched baseline and publish the Global baseline selection first, as the Global configuration recipe does. The saved Reviewer is configuration only; this recipe never proves Reviewer execution. `scratch/verify-global-checkpoints.sh RUN_DIR` drives every step below when present.

- **Check readiness.** Run `.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"` and require `doctor: ready`.
- **Publish the Global selection.** Capture `review-party init --global --baseline --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --yes` at `global-checkpoints/01-init-global-baseline.txt`.
- **Declare the Global Checkpoint.** Capture `review-party config checkpoint set pre-push --target global --integration git --format json --yes` at `global-checkpoints/02-set-global-checkpoint.txt`. The single change has field `checkpoints.pre-push`, scope `global`, and the Global path. `review-party config file show --scope global --format json` at `03-file-show-global.txt` holds the Checkpoint.
- **Read the enrollment offer.** Capture `review-party init --repo "$run_dir/runtime/repository"` at `04-init-unenrolled.txt`. The report ends with `Enroll this repository in your Global Checkpoints: review-party init --enroll --yes --repo` and the repository path, and `.reviewparty` does not exist afterwards.
- **Confirm the unenrolled hook is silent.** Pipe one git pre-push ref line into `review-party checkpoint hook git pre-push origin url` with the owned environment and capture at `05-hook-unenrolled-direct.txt`. It exits `0` with empty stdout and stderr.
- **Enroll.** Capture `review-party init --enroll --yes --repo "$run_dir/runtime/repository"` at `06-init-enroll.txt`. The Plan shows `repository schema_version` from `<absent>` to `1`, then the ready report ends with `Checkpoint pre-push has no git hook`. `$run_dir/runtime/repository/.reviewparty/config.json` is exactly `{"schema_version": 1}` pretty-printed. Capture the same command again at `08-init-enroll-again.txt`; it prints that the repository is already enrolled and changes nothing.
- **Install the hook and push.** Capture `review-party checkpoint install git --repo "$run_dir/runtime/repository" --yes` at `09-install-git.txt`. Create a bare remote beside the repository, commit an unreviewed `one.go`, and capture `git push` with the run's `bin` directory first on `PATH` at `10-push-refused.txt`, with exit-on-error disabled. It exits `1` and stderr begins `review-party: pre-push Checkpoint (declared in` followed by the Global path, the unreviewed line count, and the `review-party run --unreviewed` command.
- **Read the declaring file.** Capture `review-party checkpoint check pre-push --repo "$run_dir/runtime/repository" --base BASE --format json` at `11-check-json.txt`. `declared_in.source` is `global` and `declared_in.path` is the Global path. The human form at `12-check-human.txt` prints `declared in: global` and the path on its second line.

Negative controls: Create a second repository without running `init`, copy the installed `pre-push` hook into its `.git/hooks`, and capture its push at `13-push-unenrolled-passes.txt`. It exits `0` and the hook prints nothing. Run doctor afterwards.

## Gotchas

- A Repository `checkpoints` field, even `{}`, turns the Global Checkpoints off for that repository. `checkpoint check` then reports the Repository declaration, and the human report prints no `declared in` line.
- The hook needs `review-party` on `PATH`. Without it the installed hook warns and allows the push, which is the documented cannot-tell outcome, not a pass of the Checkpoint.
